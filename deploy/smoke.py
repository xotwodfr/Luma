#!/usr/bin/env python3
"""Seed a scratch Luma server through its public API, or verify a restored copy against the seed.

  smoke.py seed   BASE_URL STATE_FILE INVITE INVITE INVITE
  smoke.py verify BASE_URL STATE_FILE
Stdlib only. Never point this at production: it registers test users.
"""
import hashlib, json, struct, sys, urllib.request, urllib.error, uuid, zlib

def call(base, method, path, token=None, body=None, headers=None, raw=None, ctype=None):
    data = raw if raw is not None else (json.dumps(body).encode() if body is not None else None)
    req = urllib.request.Request(base + path, data=data, method=method)
    if body is not None:
        req.add_header("Content-Type", "application/json")
    if ctype:
        req.add_header("Content-Type", ctype)
    if token:
        req.add_header("Authorization", "Bearer " + token)
    for k, v in (headers or {}).items():
        req.add_header(k, v)
    try:
        with urllib.request.urlopen(req) as r:
            return r.status, r.read(), r.headers
    except urllib.error.HTTPError as e:
        return e.code, e.read(), e.headers

def jcall(*a, expect=(200, 201), **kw):
    status, raw, _ = call(*a, **kw)
    if status not in expect:
        sys.exit(f"{a[1]} {a[2]}: {status} {raw[:200]!r}")
    return json.loads(raw) if raw else {}

def png(w, h):
    rows = b"".join(b"\x00" + bytes([(x * 37 + y * 11) % 256 for x in range(w * 3)]) for y in range(h))
    chunk = lambda t, d: struct.pack(">I", len(d)) + t + d + struct.pack(">I", zlib.crc32(t + d))
    return b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", w, h, 8, 2, 0, 0, 0)) + chunk(b"IDAT", zlib.compress(rows)) + chunk(b"IEND", b"")

def upload(base, token, conv, name, data):
    b = uuid.uuid4().hex
    body = (f'--{b}\r\nContent-Disposition: form-data; name="file"; filename="{name}"\r\n'
            f"Content-Type: application/octet-stream\r\n\r\n").encode() + data + f"\r\n--{b}--\r\n".encode()
    return jcall(base, "POST", f"/v1/files?conversation_id={conv}", token, raw=body, ctype=f"multipart/form-data; boundary={b}")

def send(base, token, conv, body, file_id=None):
    msg = {"client_id": str(uuid.uuid4()), "kind": "file" if file_id else "text", "body": body}
    if file_id:
        msg["meta"] = {"file_id": file_id}
    return jcall(base, "POST", f"/v1/conversations/{conv}/messages", token, msg)

def seed(base, state_file, invites):
    tok = {}
    for name, inv in zip(["alice", "bob", "carol"], invites):
        r = jcall(base, "POST", "/v1/auth/register", body={"invite": inv, "username": name, "display_name": name.title(),
                  "password": f"staging-password-{name}", "device_name": "smoke", "platform": "linux"},
                  headers={"CF-Connecting-IP": "203.0.113.7"})
        tok[name] = r["token"]
    group = jcall(base, "POST", "/v1/conversations", tok["alice"], {"kind": "group", "title": "Staging", "usernames": ["bob", "carol"]})["id"]
    dm = jcall(base, "POST", "/v1/conversations", tok["alice"], {"kind": "direct", "username": "bob"})["id"]
    for i in range(20):
        send(base, tok["alice" if i % 2 else "bob"], group, f"group message {i} with searchable kiwi")
    for i in range(5):
        send(base, tok["alice"], dm, f"dm {i}")
    files = {}
    for name, data in [("photo.png", png(64, 48)), ("notes.txt", b"plain notes\n" * 100)]:
        f = upload(base, tok["alice"], group, name, data)
        send(base, tok["alice"], group, f"here is {name}", f["id"])
        files[f["id"]] = hashlib.sha256(data).hexdigest()
    jcall(base, "DELETE", f"/v1/conversations/{group}/members/" + jcall(base, "GET", "/v1/users/carol", tok["alice"])["id"], tok["alice"])
    late = upload(base, tok["alice"], group, "after-carol.txt", b"carol must not see this")
    send(base, tok["alice"], group, "posted after carol left", late["id"])
    files[late["id"]] = hashlib.sha256(b"carol must not see this").hexdigest()
    status, _, _ = call(base, "GET", f"/v1/files/{late['id']}", tok["carol"])
    assert status == 404, f"removed member fetched a later file: {status}"
    hist = {c: len(jcall(base, "GET", f"/v1/conversations/{c}/messages?limit=100", tok["alice"])["messages"]) for c in (group, dm)}
    state = {"group": group, "dm": dm, "files": files, "late_file": late["id"], "history": hist}
    json.dump(state, open(state_file, "w"), indent=1)
    print(f"seeded: group {hist[group]} messages, dm {hist[dm]} messages, {len(files)} files; window check ok")

def verify(base, state_file):
    st = json.load(open(state_file))
    tok = {n: jcall(base, "POST", "/v1/auth/login", body={"username": n, "password": f"staging-password-{n}", "device_name": "verify", "platform": "linux"})["token"] for n in ("alice", "carol")}
    for conv, n in st["history"].items():
        got = len(jcall(base, "GET", f"/v1/conversations/{conv}/messages?limit=100", tok["alice"])["messages"])
        assert got == n, f"{conv}: {got} messages after restore, {n} before"
    for fid, digest in st["files"].items():
        status, raw, _ = call(base, "GET", f"/v1/files/{fid}", tok["alice"])
        assert status == 200 and hashlib.sha256(raw).hexdigest() == digest, f"file {fid}: {status}"
    assert call(base, "GET", f"/v1/files/{st['late_file']}", tok["carol"])[0] == 404, "window not enforced after restore"
    hits = jcall(base, "GET", "/v1/search?q=kiwi&limit=50", tok["alice"])["results"]
    assert len(hits) == 20, f"search after restore: {len(hits)} hits"
    print(f"verified via API: history {st['history']}, {len(st['files'])} files byte-identical, window enforced, search ok")

if __name__ == "__main__":
    {"seed": lambda: seed(sys.argv[2], sys.argv[3], sys.argv[4:7]), "verify": lambda: verify(sys.argv[2], sys.argv[3])}[sys.argv[1]]()
