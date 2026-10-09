#!/bin/bash
# Run on main node. SQLite online backup produces a consistent database snapshot.
# Output is protected and contains member credentials/session data.
set -euo pipefail
export KUBECONFIG=${KUBECONFIG:-/home/utmist/.kube/config}
backup_dir=${1:-/home/utmist/.local/share/mist-backups}
mkdir -p "$backup_dir"
chmod 0700 "$backup_dir"
umask 077
file="$backup_dir/auth-$(date -u +%Y%m%dT%H%M%S).sqlite"
trap 'rm -f "$file.partial"' EXIT
k3s kubectl exec -n mist-system deployment/mist-auth -- node --input-type=module -e '
import Database from "better-sqlite3";
import {readFileSync,unlinkSync} from "node:fs";
const db=new Database("/data/auth.sqlite");
await db.backup("/tmp/mist-auth-backup.sqlite");
process.stdout.write(readFileSync("/tmp/mist-auth-backup.sqlite"));
unlinkSync("/tmp/mist-auth-backup.sqlite");db.close();
' > "$file.partial"
test -s "$file.partial"
mv "$file.partial" "$file"
echo "Auth backup saved: $file"
