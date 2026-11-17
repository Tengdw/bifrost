package auth

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/jackc/pgx/v5"
)

const ceremonyLimit = 256

func (s *Service) startCeremony(w http.ResponseWriter, r *http.Request, purpose string, p principal, data *webauthn.SessionData, options any) {
	token, hash := newToken()
	now := time.Now()
	previous, previousErr := s.readHash(r, purpose)
	s.mu.Lock()
	if previousErr == nil {
		delete(s.ceremonies, previous)
	}
	for id, c := range s.ceremonies {
		if !now.Before(c.expires) {
			delete(s.ceremonies, id)
		}
	}
	if len(s.ceremonies) >= ceremonyLimit {
		s.mu.Unlock()
		tooManyRequests(w)
		return
	}
	s.ceremonies[hash] = ceremony{data: *data, purpose: purpose, sessionHash: p.hash, expires: now.Add(challengeLifetime)}
	s.mu.Unlock()
	s.setCookie(w, purpose, token, int(challengeLifetime/time.Second))
	jsonResponse(w, http.StatusOK, options)
}

func (s *Service) finishCeremony(w http.ResponseWriter, r *http.Request, purpose string, sessionHash [sha256.Size]byte) (webauthn.SessionData, bool) {
	hash, err := s.readHash(r, purpose)
	s.setCookie(w, purpose, "", -1)
	if err != nil {
		problem(w, http.StatusBadRequest, "ceremony_expired")
		return webauthn.SessionData{}, false
	}
	s.mu.Lock()
	c, ok := s.ceremonies[hash]
	delete(s.ceremonies, hash)
	s.mu.Unlock()
	if !ok || !time.Now().Before(c.expires) || c.purpose != purpose || c.sessionHash != sessionHash {
		problem(w, http.StatusBadRequest, "ceremony_expired")
		return webauthn.SessionData{}, false
	}
	return c.data, true
}

func (s *Service) passkeyLoginOptions(w http.ResponseWriter, r *http.Request) {
	options, data, err := s.webauthn.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		authError(w, err)
		return
	}
	s.startCeremony(w, r, "passkey_login", principal{}, data, options)
}

func (s *Service) passkeyLogin(w http.ResponseWriter, r *http.Request) {
	data, ok := s.finishCeremony(w, r, "passkey_login", [sha256.Size]byte{})
	if !ok {
		return
	}
	response, err := protocol.ParseCredentialRequestResponse(r)
	if err != nil {
		problem(w, http.StatusBadRequest, "invalid_passkey_response")
		return
	}
	ctx := r.Context()
	var value string
	err = withAuthLock(ctx, s.pool, func(tx pgx.Tx) error {
		var loadErr error
		user, credential, err := s.webauthn.ValidatePasskeyLogin(func(_ []byte, id []byte) (webauthn.User, error) {
			if len(id) != digestBytes {
				return nil, errUnauthorized
			}
			a, e := loadAdmin(ctx, tx, id)
			loadErr = e
			return a, e
		}, data, response)
		if loadErr != nil {
			return loadErr
		}
		if err != nil || credential.Authenticator.CloneWarning {
			return errUnauthorized
		}
		if err = updatePasskey(ctx, tx, user.WebAuthnID(), credential); err != nil {
			return err
		}
		value, err = s.issueSession(r, tx, user.WebAuthnID())
		return err
	})
	s.finishLogin(w, value, err)
}

func (s *Service) passkeyList(w http.ResponseWriter, r *http.Request, p principal) {
	items, err := listPasskeys(r.Context(), s.pool, p.id)
	if err != nil {
		authError(w, err)
		return
	}
	jsonResponse(w, http.StatusOK, items)
}

func requireRecentLogin(w http.ResponseWriter, p principal) bool {
	if time.Since(p.createdAt) > recentLogin {
		problem(w, http.StatusPreconditionRequired, "reauth_required")
		return false
	}
	return true
}

func (s *Service) passkeyRegisterOptions(w http.ResponseWriter, r *http.Request, p principal) {
	if !requireRecentLogin(w, p) {
		return
	}
	a, err := loadAdmin(r.Context(), s.pool, p.id)
	if err != nil {
		authError(w, err)
		return
	}
	if len(a.Credentials) >= 32 {
		problem(w, http.StatusConflict, "passkey_limit")
		return
	}
	options, data, err := s.webauthn.BeginRegistration(a,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		webauthn.WithExclusions(webauthn.Credentials(a.Credentials).CredentialDescriptors()),
	)
	if err != nil {
		authError(w, err)
		return
	}
	s.startCeremony(w, r, "passkey_register", p, data, options)
}

var errPasskeyConflict = errors.New("passkey already saved or limit reached")
var errInvalidPasskey = errors.New("passkey response failed verification")

func (s *Service) passkeyRegister(w http.ResponseWriter, r *http.Request, p principal) {
	if !requireRecentLogin(w, p) {
		return
	}
	data, ok := s.finishCeremony(w, r, "passkey_register", p.hash)
	if !ok {
		return
	}
	if !bytes.Equal(data.UserID, p.id) {
		authError(w, errUnauthorized)
		return
	}
	response, err := protocol.ParseCredentialCreationResponse(r)
	if err != nil {
		problem(w, http.StatusBadRequest, "invalid_passkey_response")
		return
	}
	ctx := r.Context()
	err = withAuthLock(ctx, s.pool, func(tx pgx.Tx) error {
		if err := sessionStillActive(ctx, tx, p); err != nil {
			return err
		}
		a, err := loadAdmin(ctx, tx, p.id)
		if err != nil {
			return err
		}
		if len(a.Credentials) >= 32 {
			return errPasskeyConflict
		}
		credential, err := s.webauthn.CreateCredential(a, data, response)
		if err != nil {
			return errInvalidPasskey
		}
		payload, err := json.Marshal(credential)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `insert into bifrost.auth_passkeys(credential_id,user_id,credential)
			values($1,$2,$3) on conflict(credential_id) do nothing`, credential.ID, p.id, payload)
		if err == nil && tag.RowsAffected() == 0 {
			return errPasskeyConflict
		}
		return err
	})
	switch {
	case errors.Is(err, errPasskeyConflict):
		problem(w, http.StatusConflict, "passkey_conflict")
	case errors.Is(err, errInvalidPasskey):
		problem(w, http.StatusBadRequest, "invalid_passkey_response")
	case err != nil:
		authError(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Service) passkeyDelete(w http.ResponseWriter, r *http.Request, p principal) {
	if !requireRecentLogin(w, p) {
		return
	}
	id, err := tokenEncoding.DecodeString(r.PathValue("id"))
	if err != nil || len(id) == 0 || len(id) > 1023 {
		problem(w, http.StatusBadRequest, "invalid_passkey_id")
		return
	}
	ctx := r.Context()
	err = withAuthLock(ctx, s.pool, func(tx pgx.Tx) error {
		if err := sessionStillActive(ctx, tx, p); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "delete from bifrost.auth_passkeys where credential_id=$1 and user_id=$2", id, p.id)
		return err
	})
	if err != nil {
		authError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
