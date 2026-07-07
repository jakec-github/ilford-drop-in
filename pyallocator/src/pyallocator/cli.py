"""Command-line entrypoint: JSON in (stdin or --input), JSON out (stdout
or --output).

Exit codes:
    0 — any well-formed run, INCLUDING an INFEASIBLE model (success=false
        in the output JSON; the operator decides what to do).
    1 — crash or invalid input (bad JSON, contract violation, unknown
        preallocated volunteer id). An error JSON is still written when
        possible, and the message goes to stderr.
"""

from __future__ import annotations

import argparse
import json
import sys
from typing import TextIO

from .api import solve
from .problem import ProblemError
from .serialization import InputError, output_to_dict, parse_input


def _write_error(out: TextIO, message: str) -> None:
    json.dump(
        {
            "solver_status": "",
            "success": False,
            "error": message,
            "objective_value": 0,
            "shifts": [],
        },
        out,
    )
    out.write("\n")


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        prog="pyallocator", description="CP-SAT rota allocator"
    )
    parser.add_argument(
        "--input", help="read input JSON from this file instead of stdin"
    )
    parser.add_argument(
        "--output", help="write output JSON to this file instead of stdout"
    )
    args = parser.parse_args(argv)

    out = open(args.output, "w") if args.output else sys.stdout
    try:
        try:
            if args.input:
                with open(args.input) as f:
                    data = json.load(f)
            else:
                data = json.load(sys.stdin)
            allocation_input = parse_input(data)
            output = solve(allocation_input)
        except (json.JSONDecodeError, InputError, ProblemError, OSError) as exc:
            message = f"{type(exc).__name__}: {exc}"
            _write_error(out, message)
            print(message, file=sys.stderr)
            return 1

        json.dump(output_to_dict(output), out)
        out.write("\n")
        return 0
    finally:
        if args.output:
            out.close()


if __name__ == "__main__":
    sys.exit(main())
