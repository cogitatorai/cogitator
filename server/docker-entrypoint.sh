#!/bin/sh
set -e

# Drop privileges to the unprivileged "cogitator" user.
#
# SaaS tenants run on Fly.io, which mounts the persistent volume at /data
# owned by root regardless of the image's build-time ownership. So when the
# container starts as root we make sure the workspace is writable by cogitator
# (uid 1000), then re-exec as that user via su-exec. When the container is
# already started as a non-root user (e.g. `docker run --user`), we just exec
# the binary directly.
WORKSPACE="${COGITATOR_WORKSPACE_PATH:-/data}"

if [ "$(id -u)" = "0" ]; then
	# Only walk the tree when it isn't already cogitator-owned. A fresh Fly
	# volume mounts root-owned (chown needed once); on later cold starts the
	# top dir is already 1000, so we skip the recursive chown and keep
	# auto-start wake latency low on large workspaces. A genuine failure
	# (read-only / NFS mount) is surfaced, not swallowed, so the cause is
	# visible instead of a later opaque write error.
	if [ "$(stat -c %u "$WORKSPACE" 2>/dev/null || echo unknown)" != "1000" ]; then
		chown -R cogitator:cogitator "$WORKSPACE" ||
			echo "warning: could not chown $WORKSPACE to cogitator; server may be unable to write" >&2
	fi
	exec su-exec cogitator cogitator "$@"
fi

exec cogitator "$@"
