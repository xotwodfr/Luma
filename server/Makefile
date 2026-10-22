.PHONY: testdb-up testdb-down test

# Disposable PostgreSQL for tests only. Never points at a production volume.
testdb-up:
	@docker ps --format '{{.Names}}' | grep -q '^luma-test-pg$$' || \
	  (docker start luma-test-pg 2>/dev/null || \
	   docker run -d --name luma-test-pg -p 127.0.0.1:55432:5432 -e POSTGRES_PASSWORD=test postgres:17-alpine)

testdb-down:
	docker rm -f luma-test-pg

test: testdb-up
	go test -race ./...
