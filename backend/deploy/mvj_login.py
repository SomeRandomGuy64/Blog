#!/usr/bin/env python3
"""Sign the MvJ sync in, from your own machine, without the server ever seeing the password.

    python3 backend/deploy/mvj_login.py root@<server>

Reads FAZOND_MVJ_SUPABASE / _ANON_KEY / _EMAIL (and FAZOND_DATA) from the server's
/etc/fazond/env, asks for the MvJ password (hidden), signs in the way the MvJ site's login form
does, then pipes only the session's refresh token to `fazond mvj-token` on the server and
restarts fazond so the sync uses it at once. Run it again whenever the journal says
"no MvJ session" or "MvJ session refresh: …".
"""
import getpass
import json
import subprocess
import sys
import urllib.error
import urllib.request

if len(sys.argv) != 2:
    sys.exit(__doc__)
server = sys.argv[1]

out = subprocess.run(["ssh", server, "grep -E '^FAZOND_(MVJ_[A-Z_]+|DATA)=' /etc/fazond/env"],
                     capture_output=True, text=True, check=True).stdout
conf = dict(line.split("=", 1) for line in out.splitlines() if "=" in line)
try:
    supabase, anon, email = conf["FAZOND_MVJ_SUPABASE"].rstrip("/"), conf["FAZOND_MVJ_ANON_KEY"], conf["FAZOND_MVJ_EMAIL"]
except KeyError as e:
    sys.exit(f"{e.args[0]} is not set in {server}:/etc/fazond/env")
data = conf.get("FAZOND_DATA", "/var/lib/fazond")

password = getpass.getpass(f"MvJ password for {email}: ")
req = urllib.request.Request(supabase + "/auth/v1/token?grant_type=password", method="POST",
                             data=json.dumps({"email": email, "password": password}).encode(),
                             headers={"apikey": anon, "Content-Type": "application/json"})
try:
    with urllib.request.urlopen(req, timeout=30) as r:
        refresh = json.load(r)["refresh_token"]
except urllib.error.HTTPError as e:
    sys.exit(f"Sign-in failed ({e.code}): {e.read().decode(errors='replace')[:300]}")
del password

subprocess.run(["ssh", server, f"runuser -u fazond -- env FAZOND_DATA={data} fazond mvj-token && systemctl restart fazond"],
               input=refresh + "\n", text=True, check=True)
print("Done: fazond restarted with the new session. Check: journalctl -u fazond --since -2min | grep -i mvj")
