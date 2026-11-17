.PHONY: dev deploy admin-recover
dev:
	python3 scripts/launch.py dev
deploy:
	python3 scripts/launch.py deploy
admin-recover:
	python3 scripts/launch.py recover

.PHONY: dev-admin-recover
dev-admin-recover:
	python3 scripts/launch.py dev-recover
