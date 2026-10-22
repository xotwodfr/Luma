#!/usr/bin/env python3
"""Seed a throwaway Luma server with the conversations from the M1 design preview.

Usage: seed-demo.py BASE_URL INVITES_FILE OUT_DIR
Writes OUT_DIR/session-<user>.json (token files for LUMA_INSECURE_TOKEN_FILE) and
OUT_DIR/backdate.sql (gives messages realistic times; the API always stamps "now").
Only ever point this at a disposable test database.
"""
import json
import sys
import urllib.request
import uuid

base, invites_file, out = sys.argv[1], sys.argv[2], sys.argv[3]
invites = open(invites_file).read().split()

people = {
    "folkes": "Folkes",
    "mira": "Mira Okafor",
    "elif": "Elif Demir",
    "dad": "Dad",
    "theo": "Theo Lindqvist",
    "noor": "Noor Haddad",
    "felix": "Felix Brandt",
}

# (other, [(day_offset, "HH:MM", sender, body)], i_read_all, they_read_all)
threads = [
    ("felix", [(-3, "16:20", "felix", "See you at the workshop")], True, True),
    ("noor", [(-2, "19:02", "noor", "That piece on type scales was exactly what I needed"),
              (-2, "19:10", "folkes", "Thanks, that helped a lot")], True, True),
    ("theo", [(0, "10:47", "theo", "Did the login fix make it into the build?"),
              (0, "10:52", "folkes", "It did, 0.9.3 has it")], True, False),
    ("dad", [(0, "11:02", "dad", "Landed. Call you from the hotel.")], False, True),
    ("elif", [(0, "12:12", "elif", "Did the landlord send the new floor plan?"),
              (0, "12:30", "folkes", "Yes, it came this morning"),
              (0, "12:31", "folkes", "The second bedroom is the one facing the courtyard"),
              (0, "12:41", "elif", "It’s bigger than I thought"),
              (0, "12:41", "elif", "Let’s go see it Thursday")], True, True),
    ("mira", [(-1, "21:16", "folkes", "Found the lake from your photos. 40 minutes from mine"),
              (-1, "21:20", "mira", "Told you it was close"),
              (0, "13:58", "mira", "Are we still on for the lake on Sunday?"),
              (0, "13:58", "mira", "Forecast says 19° and clear after ten"),
              (0, "14:01", "folkes", "Yes. I’ll pick you up at 9:30"),
              (0, "14:01", "folkes", "Bring the film camera, the early light should be good"),
              (0, "14:08", "mira", "Already loaded a roll of Portra")], False, False),
]


def call(method, path, token=None, body=None):
    req = urllib.request.Request(base + path, method=method, data=json.dumps(body).encode() if body is not None else None)
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", "Bearer " + token)
    with urllib.request.urlopen(req) as r:
        raw = r.read()
        return json.loads(raw) if raw else {}


sessions = {}
for (username, name), invite in zip(people.items(), invites):
    r = call("POST", "/v1/auth/register", body={"invite": invite, "username": username, "display_name": name,
                                                 "password": "demo-password-" + username, "platform": "linux"})
    sessions[username] = r
    me = {"id": r["user"]["id"], "username": username, "displayName": name,
          "initials": "".join(w[0] for w in name.split()[:1] + name.split()[1:][-1:]).upper()}
    json.dump({"token": r["token"], "me": me}, open(f"{out}/session-{username}.json", "w"))

sql = []
for other, msgs, i_read, they_read in threads:
    me_tok, other_tok = sessions["folkes"]["token"], sessions[other]["token"]
    conv = call("POST", "/v1/conversations", me_tok, {"kind": "direct", "username": other})["id"]
    last_mine = last = 0
    for day, hhmm, sender, body in msgs:
        tok = me_tok if sender == "folkes" else other_tok
        m = call("POST", f"/v1/conversations/{conv}/messages", tok, {"client_id": str(uuid.uuid4()), "kind": "text", "body": body})
        last = m["seq"]
        if sender == "folkes":
            last_mine = m["seq"]
        sql.append(f"UPDATE messages SET created_at = (current_date + {day}) + time '{hhmm}' + interval '{m['seq']} seconds' WHERE id = '{m['id']}';")
    if i_read:
        call("POST", f"/v1/conversations/{conv}/read", me_tok, {"seq": last})
    their_read = last if they_read else last_mine - 1 if last_mine else 0
    if their_read > 0:
        call("POST", f"/v1/conversations/{conv}/read", other_tok, {"seq": their_read})
sql.append("UPDATE conversations c SET last_activity_at = (SELECT max(created_at) FROM messages m WHERE m.conversation_id = c.id);")
open(f"{out}/backdate.sql", "w").write("\n".join(sql) + "\n")
print(f"seeded {len(people)} people, {len(threads)} conversations")
