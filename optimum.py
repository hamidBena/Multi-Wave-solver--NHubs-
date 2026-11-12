import json
import math
import argparse
from pathlib import Path

"""Solve a hub assignment instance with PuLP and HiGHS/CBC.

For large instances this is a reference MIP model: use --time-limit to obtain
the best feasible solution found so far rather than expecting a proof quickly.
"""

import pulp

parser = argparse.ArgumentParser(description="Solve a hub assignment instance exactly.")
parser.add_argument(
    "data_path",
    nargs="?",
    default="data/data_25.json",
    help="JSON dataset path, relative to the project directory by default",
)
parser.add_argument(
    "--threads",
    type=int,
    default=None,
    help="solver worker threads (default: solver choice)",
)
parser.add_argument(
    "--time-limit",
    type=float,
    default=None,
    help="maximum solve time in seconds",
)
parser.add_argument(
    "--gap-rel",
    type=float,
    default=0,
    help="relative MIP gap; 0 requires proven optimality",
)
parser.add_argument("--quiet", action="store_true", help="suppress solver logs")
args = parser.parse_args()
DATA_PATH = Path(args.data_path)
if not DATA_PATH.is_absolute():
    DATA_PATH = Path(__file__).resolve().parent / DATA_PATH
with DATA_PATH.open(encoding="utf-8") as data_file:
    d = json.load(data_file)
alpha = d["parameters"]["alpha"]
P = d["parameters"]["numberOfHubs"]

nodes = sorted(d["nodes"], key=lambda t: t["node"])
X = [t["x"] for t in nodes]
Y = [t["y"] for t in nodes]
n = len(X)
configured_capacities = d["parameters"].get("hubCapacities")
if configured_capacities is None:
    capacities = [n // P + (slot < n % P) for slot in range(P)]
else:
    capacities = [int(capacity) for capacity in configured_capacities]
    if len(capacities) != P or any(capacity < 1 for capacity in capacities):
        raise ValueError("hubCapacities must contain one positive value per hub")
    if sum(capacities) != n:
        raise ValueError(f"hubCapacities must sum to {n}, got {sum(capacities)}")
R = range(n)

q = [[0] * n for _ in R]
for e in d["edges"]:
    from_node = e["fromNode"]
    to_node = e["toNode"]
    if from_node != to_node:
        i, j = sorted((from_node, to_node))
        q[i][j] += e["q"]

dist = [[math.hypot(X[i]-X[j], Y[i]-Y[j]) for j in R] for i in R]
origin = [sum(q[i]) for i in R]
w = [origin[i] + sum(q[j][i] for j in R) for i in R]

m = pulp.LpProblem("hub", pulp.LpMinimize)


def variable_dicts(name, indices, **kwargs):
    """Create indexed variables with both PuLP 3.x and 4.x APIs."""
    factory = getattr(m, "add_variable_dicts", None)
    if factory is not None:
        return factory(name, indices, **kwargs)
    return pulp.LpVariable.dicts(name, indices, **kwargs)


x = variable_dicts("x", (R, R), cat="Binary")  # x[i][k]: i assigned to hub k
f = variable_dicts("f", (R, R, R), lowBound=0)  # flow from origin i on hub arc k->l
z = variable_dicts("z", (R, range(P)), cat="Binary")

m += (pulp.lpSum(w[i]*dist[i][k]*x[i][k] for i in R for k in R)
      + alpha*pulp.lpSum(dist[k][l]*f[i][k][l]
                         for i in R for k in R for l in R if k != l))

for i in R:
    m += pulp.lpSum(x[i][k] for k in R) == 1
    for k in R:
        m += x[i][k] <= x[k][k]
m += pulp.lpSum(x[k][k] for k in R) == P
for k in R:
    m += pulp.lpSum(z[k][slot] for slot in range(P)) == x[k][k]
for slot in range(P):
    m += pulp.lpSum(z[k][slot] for k in R) == 1
for k in R:
    m += pulp.lpSum(x[i][k] for i in R) == pulp.lpSum(
        capacities[slot] * z[k][slot] for slot in range(P)
    )

for i in R:
    for k in R:
        m += (pulp.lpSum(f[i][k][l] for l in R if l != k)
              - pulp.lpSum(f[i][l][k] for l in R if l != k)
              == origin[i]*x[i][k] - pulp.lpSum(q[i][j]*x[j][k] for j in R))

available_solvers = set(pulp.listSolvers(onlyAvailable=True))
if "HiGHS" in available_solvers:
    solver = pulp.getSolver(
        "HiGHS",
        msg=not args.quiet,
        threads=args.threads,
        timeLimit=args.time_limit,
        gapRel=args.gap_rel,
    )
elif "PULP_CBC_CMD" in available_solvers:
    solver = pulp.getSolver(
        "PULP_CBC_CMD",
        msg=not args.quiet,
        threads=args.threads,
        timeLimit=args.time_limit,
        gapRel=args.gap_rel,
    )
else:
    raise RuntimeError(
        "No supported PuLP solver is available. Install CBC or HiGHS."
    )

solve_result = m.solve(solver)
raw_status = getattr(solve_result, "status", getattr(m, "status", solve_result))
status = raw_status.name if hasattr(raw_status, "name") else pulp.LpStatus[raw_status]
objective = (
    solve_result.objective
    if hasattr(solve_result, "objective")
    else pulp.value(m.objective)
)
print(f"{status} {objective} ({DATA_PATH.name}, {n} nodes)")