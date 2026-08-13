#!/usr/bin/env python3
"""Answer a minted availability round with plausible random availability.

This is a test-data helper for a dev or test stack, not a production tool. It
reads a round back from the admin API, then submits an answer through each
volunteer's own public link — the same endpoint the form posts to, so what lands
in the database is a real generation rather than seeded rows.

Usage:
    scripts/fake-availability.py                      # answer the latest round
    scripts/fake-availability.py --mean 0.6 --sd 0.16
    scripts/fake-availability.py --rota <rotaId> --seed 1 --dry-run

Admin session: by default it logs in at /auth/login, which against the
credential-free dev stack mints a session with no Google round trip (see
docs/agents/dev-stack.md). Against anything with real OIDC that will not work —
pass an existing session cookie with --session instead.

The rate is drawn per answerer rather than fixed, so the round has volunteers
who are nearly always free and volunteers who are nearly never free instead of
everyone hovering at the mean. --mean is the average share of open shifts said
yes to; --sd is how far individuals spread around it.

A tenth are left unanswered by default, because a real round always has some
who never reply and the screens that chase them need something to chase. Pass
--reply-rate 1 for a round everyone answered.
"""

import argparse
import http.cookiejar
import json
import os
import random
import sys
import urllib.error
import urllib.parse
import urllib.request

DEFAULT_API_URL = "http://localhost:8080"

# The cookie the server issues an admin session in (pkg/api/auth.go).
SESSION_COOKIE = "session"


def beta_params(mean: float, sd: float) -> tuple[float, float]:
    """Beta shape parameters for a given mean and standard deviation.

    A beta is used rather than a clipped normal because availability is a
    proportion: it cannot stray outside [0, 1], and clipping a normal would pile
    up mass exactly at 0 and 1 instead of tapering towards them.
    """
    variance = sd**2
    # Beyond this the distribution has no solution — a proportion cannot be that
    # spread out around that mean without leaving [0, 1].
    ceiling = mean * (1 - mean)
    if variance >= ceiling:
        raise ValueError(
            f"sd {sd} is too wide for mean {mean}: it must be under {ceiling ** 0.5:.3f}"
        )
    concentration = ceiling / variance - 1
    return mean * concentration, (1 - mean) * concentration


class Client:
    """The API, with whatever session the caller established attached."""

    def __init__(self, base_url: str, session: str | None):
        self.base_url = base_url.rstrip("/")
        # A supplied session is sent as a header rather than put in the jar.
        # http.cookiejar will not attach a cookie it did not receive itself
        # unless every domain attribute is built to match its policy — and for a
        # dotless host like localhost it silently declines even then. The header
        # is what the jar would have produced anyway.
        self.session = session
        self.jar = http.cookiejar.CookieJar()
        self.opener = urllib.request.build_opener(
            urllib.request.HTTPCookieProcessor(self.jar)
        )

    def fetch(self, method: str, path: str, body: dict | None = None) -> bytes:
        data = None
        headers = {}
        if self.session is not None:
            headers["Cookie"] = f"{SESSION_COOKIE}={self.session}"
        if body is not None:
            data = json.dumps(body).encode()
            headers["Content-Type"] = "application/json"
        req = urllib.request.Request(
            self.base_url + path, data=data, headers=headers, method=method
        )
        try:
            with self.opener.open(req) as resp:
                return resp.read()
        except urllib.error.HTTPError as err:
            # The API answers errors in JSON under /api, and its message names
            # the problem far better than the status code does.
            detail = err.read().decode(errors="replace")
            try:
                detail = json.loads(detail).get("error", detail)
            except json.JSONDecodeError:
                pass
            raise SystemExit(f"{method} {path} failed: HTTP {err.code} — {detail}")

    def request(self, method: str, path: str, body: dict | None = None) -> dict | None:
        raw = self.fetch(method, path, body)
        return json.loads(raw) if raw else None

    def login(self) -> None:
        """Follow /auth/login to its end, which on a dev stack sets the session.

        Fetched raw rather than as JSON: the stub login redirects to the app, so
        what comes back is the SPA's HTML, and only the cookie it set matters.
        """
        self.fetch("GET", "/auth/login")
        if not any(c.name == SESSION_COOKIE for c in self.jar):
            raise SystemExit(
                "/auth/login did not issue a session. Against a server with real "
                "Google login, pass an existing cookie with --session."
            )

    def whoami(self) -> str:
        """The admin the session belongs to, checked before anything is written.

        The server answers a bad session and a session for someone off the
        allowlist with the same 401, so this cannot say which it is — but it can
        fail on the credential rather than on the first endpoint that uses it,
        and name the account when it works.
        """
        try:
            return self.request("GET", "/auth/me")["email"]
        except SystemExit:
            raise SystemExit(
                f"the session was rejected by {self.base_url}. It is either "
                "expired, from a different server, or for an address that is "
                "not in that environment's server.adminEmails."
            )


def token_of(link: str) -> str:
    """The token out of a volunteer's link, which is its last path segment."""
    return urllib.parse.urlparse(link).path.rstrip("/").rsplit("/", 1)[-1]


def answerers(round_data: dict, per_volunteer: bool) -> list[dict]:
    """Who is going to answer, and the link each answer goes through.

    One member per group by default. A group's availability is the intersection
    over everyone in it who replied (ADR 0004), so two partners answering
    independently at 60% each leaves the group at 36% — the rate asked for is
    only the rate the allocator sees if each group speaks once.
    """
    chosen = []
    for group in round_data["groups"]:
        members = group["members"]
        if per_volunteer:
            chosen.extend(
                {"name": m["volunteerName"], "link": m["link"], "replied": m["replied"]}
                for m in members
            )
            continue
        # A group that has already spoken keeps its spokesperson, so a re-run
        # tops the round up rather than adding a second, intersecting answer.
        speaker = next((m for m in members if m["replied"]), members[0])
        chosen.append(
            {
                "name": group["name"],
                "link": speaker["link"],
                "replied": group["replied"],
            }
        )
    return chosen


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Answer a minted availability round with random availability."
    )
    parser.add_argument(
        "--api-url", default=os.environ.get("API_URL", DEFAULT_API_URL)
    )
    parser.add_argument(
        "--session",
        default=os.environ.get("SESSION"),
        help="an existing admin session cookie; omit to log in at /auth/login",
    )
    parser.add_argument("--rota", default="", help="rota id; omit for the latest")
    parser.add_argument(
        "--mean", type=float, default=0.6, help="average share of open shifts said yes to"
    )
    parser.add_argument(
        "--sd",
        type=float,
        default=0.26,
        help="how far individual rates spread around the mean",
    )
    parser.add_argument(
        "--reply-rate",
        type=float,
        default=0.9,
        help="share who answer at all; the rest are left unanswered for chasing",
    )
    parser.add_argument(
        "--per-volunteer",
        action="store_true",
        help="every volunteer answers separately, which intersects within groups",
    )
    parser.add_argument(
        "--resubmit",
        action="store_true",
        help="also answer for those who already have, appending a generation",
    )
    parser.add_argument("--seed", type=int, help="make the draw reproducible")
    parser.add_argument(
        "--dry-run", action="store_true", help="report what would be submitted"
    )
    args = parser.parse_args()

    if not 0 < args.mean < 1:
        raise SystemExit("--mean must be between 0 and 1")
    try:
        alpha, beta = beta_params(args.mean, args.sd)
    except ValueError as err:
        raise SystemExit(str(err))
    rng = random.Random(args.seed)

    # Copied out of devtools a session often arrives as `session=<value>`, or
    # wrapped in the quotes the copy button adds. Taking the value out of those
    # beats answering a 401 that cannot say which of the two went wrong.
    session = args.session
    if session is not None:
        session = session.strip().strip('"').strip("'")
        _, _, session = session.rpartition(SESSION_COOKIE + "=")
        session = session.rstrip(";").strip()
        if not session:
            raise SystemExit("--session was given but is empty")

    client = Client(args.api_url, session)
    if session is None:
        client.login()
    print(f"acting as {client.whoami()} on {client.base_url}")

    query = f"?rotaId={urllib.parse.quote(args.rota)}" if args.rota else ""
    round_data = client.request("GET", "/api/availability-rounds" + query)

    if round_data["allocated"]:
        raise SystemExit(
            f"rota {round_data['rotaId']} is allocated, so its links no longer work"
        )
    open_shifts = [s["id"] for s in round_data["shifts"] if not s["closed"]]
    if not open_shifts:
        raise SystemExit(f"rota {round_data['rotaId']} has no open shifts to answer for")

    targets = answerers(round_data, args.per_volunteer)
    if not targets:
        raise SystemExit(
            f"rota {round_data['rotaId']} has no availability requests — mint the round first"
        )

    submitted = skipped = 0
    total_yes = 0
    for target in targets:
        if target["replied"] and not args.resubmit:
            skipped += 1
            continue
        if rng.random() >= args.reply_rate:
            skipped += 1
            continue

        rate = rng.betavariate(alpha, beta)
        # Drawn independently per shift rather than as a fixed-size sample, so
        # answers vary in length the way real ones do.
        chosen = [s for s in open_shifts if rng.random() < rate]
        total_yes += len(chosen)
        submitted += 1

        print(
            f"{target['name']}: {len(chosen)}/{len(open_shifts)} shifts (rate {rate:.2f})"
        )
        if not args.dry_run:
            client.request(
                "POST",
                "/api/availability/" + token_of(target["link"]),
                {"shiftIds": chosen},
            )

    verb = "would submit" if args.dry_run else "submitted"
    summary = f"\n{verb} {submitted} answer(s), skipped {skipped}"
    # An average over nothing is not 0%, it is nothing — and printing 0% next to
    # "submitted 0" reads as a round that came back empty rather than untouched.
    if submitted:
        share = total_yes / (submitted * len(open_shifts))
        summary += f", averaging {share:.0%} of {len(open_shifts)} open shifts"
    print(summary)
    return 0


if __name__ == "__main__":
    sys.exit(main())
