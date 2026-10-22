# Tools for deploy/backup.sh and restore.sh: age for encryption, busybox tar for uploads.
# Build: docker build -t luma-backup -f deploy/backup.Dockerfile deploy
FROM alpine:3.24
RUN apk add --no-cache age
