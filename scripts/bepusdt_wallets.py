#!/usr/bin/env python3
"""Batch-add BEpusdt collection wallets through the gateway admin API.

Usage:
  python3 scripts/bepusdt_wallets.py path/to/wallets.json

The JSON file shape is documented in scripts/bepusdt_wallets.example.json.
Existing addresses are skipped so the script is safe to re-run.
"""
import json
import sys
import urllib.error
import urllib.request
from http.cookiejar import CookieJar


def main() -> int:
    if len(sys.argv) != 2:
        print(__doc__)
        return 2
    with open(sys.argv[1], "r", encoding="utf-8") as fh:
        cfg = json.load(fh)

    base = cfg["gateway_url"].rstrip("/")
    jar = CookieJar()
    opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))

    def call(path: str, payload: dict) -> dict:
        req = urllib.request.Request(
            base + path,
            data=json.dumps(payload).encode(),
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        try:
            with opener.open(req, timeout=15) as resp:
                body = resp.read().decode()
        except urllib.error.HTTPError as err:
            body = err.read().decode()
            if err.code != 200:
                return {"code": err.code, "message": body}
        try:
            return json.loads(body)
        except json.JSONDecodeError:
            return {"code": 1, "message": body}

    # Step 1: acquire the secure session via the admin entrance if provided.
    entrance = cfg.get("entrance")
    if entrance:
        opener.open(base + entrance, timeout=15).read()

    # Step 2: login.
    login = call("/api/auth/login", {"username": cfg["username"], "password": cfg["password"]})
    token = (login.get("data") or {}).get("token") if login.get("code") == 200 else None
    if not token:
        print("登录失败:", json.dumps(login, ensure_ascii=False)[:300])
        return 1
    headers = {"Content-Type": "application/json", "Authorization": token}

    def authed(path: str, payload: dict) -> dict:
        req = urllib.request.Request(
            base + path,
            data=json.dumps(payload).encode(),
            headers=headers,
            method="POST",
        )
        try:
            with opener.open(req, timeout=15) as resp:
                body = resp.read().decode()
        except urllib.error.HTTPError as err:
            body = err.read().decode()
        try:
            return json.loads(body)
        except json.JSONDecodeError:
            return {"code": 1, "message": body}

    # Step 3: existing wallets (skip duplicates).
    existing = set()
    page = 1
    while True:
        listing = authed("/api/wallet/list", {"page": page, "size": 100, "sort": "asc"})
        rows = listing.get("data")
        if not isinstance(rows, list) or not rows:
            break
        for row in rows:
            existing.add((row.get("match_addr") or row.get("address"), row.get("trade_type")))
        if len(rows) < 100:
            break
        page += 1

    added = skipped = failed = 0
    for wallet in cfg.get("wallets", []):
        trade_type = wallet["trade_type"].strip()
        address = wallet["address"].strip()
        if not address or "Your" in address or "PASTE" in address:
            print(f"[跳过] {trade_type}: 地址为空或仍为模板占位符")
            skipped += 1
            continue
        if (address, trade_type) in existing:
            print(f"[跳过] {trade_type}: 地址已存在")
            skipped += 1
            continue
        payload = {
            "name": (wallet.get("name") or trade_type)[:32],
            "remark": (wallet.get("remark") or "")[:255],
            "address": address,
            "trade_type": trade_type,
            "other_notify": int(wallet.get("other_notify", 0)),
        }
        result = authed("/api/wallet/add", payload)
        if result.get("code") == 200:
            print(f"[成功] {trade_type} {address}")
            added += 1
        else:
            print(f"[失败] {trade_type}: {json.dumps(result, ensure_ascii=False)[:200]}")
            failed += 1

    print(f"\n完成：新增 {added}，跳过 {skipped}，失败 {failed}")
    return 0 if failed == 0 else 1


if __name__ == "__main__":
    sys.exit(main())
