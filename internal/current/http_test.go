package current

import (
	"context"
	"net/http/httptest"
	"testing"
)

func TestRejectsInvalidQueriesBeforeReadingDatabase(t *testing.T) {
	svc := NewService(context.Background(), nil, nil)
	id := uuid()
	for _, q := range []string{
		"/overview?basis=latest&accountIds=" + id + "&accountIds=" + id,
		"/holdings?limit=1garbage",
		"/holdings?limit=1&limit=2",
		"/overview?basis=latest&wallet=spot",
		"/holdings?unknown=1",
	} {
		w := httptest.NewRecorder()
		svc.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/v1"+q, nil))
		if w.Code != 400 {
			t.Fatal(q, w.Code)
		}
	}
}
