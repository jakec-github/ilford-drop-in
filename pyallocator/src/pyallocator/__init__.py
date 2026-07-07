"""CP-SAT rota allocator: constraints-only v1.

Public API: solve(AllocationInput) -> AllocationOutput.
"""

from .api import solve

__all__ = ["solve"]
