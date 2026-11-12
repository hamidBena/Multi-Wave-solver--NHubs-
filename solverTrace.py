"""Interactive Multi-Wave solution depth map.

Usage:
    python depth_map.py

The 3D view is interactive with Matplotlib's rotate/pan/zoom controls. The
bottom panel is a fixed-bounds top-down contour view. The assignment floor is
computed by deterministic capacity-preserving local search for each hub set.
"""

from __future__ import annotations

import argparse
import csv
import json
import math
from collections import defaultdict
from dataclasses import dataclass
from pathlib import Path
from typing import Iterable

import matplotlib.pyplot as plt
import numpy as np
from matplotlib import cm, colors
from mpl_toolkits.mplot3d import Axes3D  # noqa: F401
from tkinter import Tk, filedialog


@dataclass(frozen=True)
class NodeData:
    x: list[float]
    y: list[float]
    quantity: list[list[float]]


@dataclass
class Point:
    wave: int
    iteration: int
    cost: float
    hubs: tuple[int, ...]
    assignment: tuple[int, ...]
    cog_x: float = 0.0
    cog_y: float = 0.0
    floor: float = 0.0
    spread: float = 1.0

    @property
    def inefficiency(self) -> float:
        return max(0.0, self.cost - self.floor)


def load_node_data(path: Path) -> NodeData:
    raw = json.loads(path.read_text(encoding="utf-8"))
    nodes = raw["nodes"]
    x = [float(node.get("x", 0.0)) for node in nodes]
    y = [float(node.get("y", 0.0)) for node in nodes]
    quantity = [[0.0] * len(nodes) for _ in nodes]
    for edge in raw.get("edges", []):
        source, target = int(edge["fromNode"]), int(edge["toNode"])
        value = float(edge.get("q", 0.0))
        quantity[source][target] += value
        quantity[target][source] += value
    parameters = raw.get("parameters", {})
    hub_count = int(parameters.get("numberOfHubs", 0))
    return NodeData(x, y, quantity)


def decode_solution(row: dict[str, str]) -> tuple[tuple[int, ...], tuple[int, ...]]:
    if row.get("solution"):
        solution = json.loads(row["solution"])
        return tuple(map(int, solution["hubs"])), tuple(map(int, solution["assign"]))
    hubs = json.loads(row["hubs"]) if row.get("hubs", "").startswith("[") else [
        int(value) for value in row["hubs"].split()
    ]
    assignment = json.loads(row["assignment"]) if row.get("assignment", "").startswith("[") else [
        int(value) for value in row["assignment"].split()
    ]
    return tuple(hubs), tuple(assignment)


def load_log(path: Path) -> list[Point]:
    with path.open(newline="", encoding="utf-8") as file:
        reader = csv.DictReader(file)
        if reader.fieldnames and len(reader.fieldnames) == 1 and "," in reader.fieldnames[0]:
            reader.fieldnames = next(csv.reader([reader.fieldnames[0].strip('"')]))
        required = {"wave", "cost"}
        if not reader.fieldnames or not required.issubset(reader.fieldnames):
            raise ValueError(f"log must contain columns: {', '.join(sorted(required))}")
        points = []
        for line, row in enumerate(reader, start=2):
            try:
                hubs, assignment = decode_solution(row)
                points.append(Point(
                    wave=int(row["wave"]),
                    iteration=int(row.get("iteration", row.get("iter", 0))),
                    cost=float(row["cost"]),
                    hubs=hubs,
                    assignment=assignment,
                ))
            except (KeyError, TypeError, ValueError, json.JSONDecodeError) as error:
                raise ValueError(f"invalid log row {line}: {error}") from error
    if not points:
        raise ValueError(f"log contains no solutions: {path}")
    return points


def dataset_path_from_log(path: Path) -> Path:
    with path.open(newline="", encoding="utf-8") as file:
        reader = csv.DictReader(file)
        if reader.fieldnames and len(reader.fieldnames) == 1 and "," in reader.fieldnames[0]:
            reader.fieldnames = next(csv.reader([reader.fieldnames[0].strip('"')]))
        if not reader.fieldnames or "dataset" not in reader.fieldnames:
            raise ValueError("solver log must contain a dataset column")
        row = next(reader, None)
        dataset = row.get("dataset", "").strip() if row else ""
    if not dataset:
        raise ValueError("solver log dataset column is empty")
    dataset_file = Path(dataset)
    if not dataset_file.is_file():
        raise ValueError(f"node data file from log does not exist: {dataset_file}")
    return dataset_file


def solution_cost(data: NodeData, hubs: tuple[int, ...], assignment: tuple[int, ...]) -> float:
    total = 0.0
    for source in range(len(assignment)):
        for target in range(source + 1, len(assignment)):
            total += pair_cost(data, assignment, source, target)
    return total


def pair_cost(data: NodeData, assignment: tuple[int, ...] | list[int],
              source: int, target: int) -> float:
    source_hub, target_hub = assignment[source], assignment[target]
    distance_source = math.hypot(data.x[source] - data.x[source_hub],
                                 data.y[source] - data.y[source_hub])
    distance_target = math.hypot(data.x[target] - data.x[target_hub],
                                 data.y[target] - data.y[target_hub])
    hub_distance = math.hypot(data.x[source_hub] - data.x[target_hub],
                              data.y[source_hub] - data.y[target_hub])
    return data.quantity[source][target] * (
        distance_source + data.alpha * hub_distance + distance_target
    )


def swapped_pair_cost(data: NodeData, assignment: list[int], left: int, right: int,
                      source: int, target: int) -> float:
    if source == left:
        source_hub = assignment[right]
    elif source == right:
        source_hub = assignment[left]
    else:
        source_hub = assignment[source]
    if target == left:
        target_hub = assignment[right]
    elif target == right:
        target_hub = assignment[left]
    else:
        target_hub = assignment[target]
    temporary = list(assignment)
    temporary[source] = source_hub
    temporary[target] = target_hub
    return pair_cost(data, temporary, source, target)


def swap_cost_delta(data: NodeData, assignment: list[int], left: int, right: int) -> float:
    old_cost = 0.0
    new_cost = 0.0
    for node in range(len(assignment)):
        if node == left or node == right:
            continue
        first, second = sorted((left, node))
        old_cost += pair_cost(data, assignment, first, second)
        new_cost += swapped_pair_cost(data, assignment, left, right, first, second)
        first, second = sorted((right, node))
        old_cost += pair_cost(data, assignment, first, second)
        new_cost += swapped_pair_cost(data, assignment, left, right, first, second)
    old_cost += pair_cost(data, assignment, left, right)
    new_cost += swapped_pair_cost(data, assignment, left, right, left, right)
    return new_cost - old_cost


def assignment_floor(data: NodeData, hubs: tuple[int, ...], seed: tuple[int, ...]) -> float:
    """Find a strong capacity-feasible floor while preserving the hub set."""
    assignment = list(seed)
    for hub in hubs:
        assignment[hub] = hub
    loads = {hub: assignment.count(hub) for hub in hubs}
    members = [node for node in range(len(assignment)) if node not in hubs]
    for node in members:
        if assignment[node] not in loads:
            assignment[node] = hubs[0]
            loads[hubs[0]] += 1
    for _ in range(max(1, len(members) * 4)):
        current = solution_cost(data, hubs, tuple(assignment))
        best = current
        best_pair: tuple[int, int] | None = None
        for left_index, left in enumerate(members):
            for right in members[left_index + 1:]:
                if assignment[left] == assignment[right]:
                    continue
                candidate = current + swap_cost_delta(data, assignment, left, right)
                if candidate < best:
                    best, best_pair = candidate, (left, right)
        if best_pair is None:
            break
        left, right = best_pair
        assignment[left], assignment[right] = assignment[right], assignment[left]
    return solution_cost(data, hubs, tuple(assignment))


def enrich(points: list[Point], data: NodeData) -> None:
    for point in points:
        if not point.hubs:
            raise ValueError(f"solution at iteration {point.iteration} has no hubs")
        point.cog_x = sum(data.x[hub] for hub in point.hubs) / len(point.hubs)
        point.cog_y = sum(data.y[hub] for hub in point.hubs) / len(point.hubs)
        point.spread = sum(math.hypot(data.x[hub] - point.cog_x, data.y[hub] - point.cog_y)
                           for hub in point.hubs) / max(1, len(point.hubs))


def bin_surface(points: Iterable[Point], bins: int = 25) -> tuple[list[float], list[float], dict[tuple[int, int], list[float]]]:
    points = list(points)
    xs, ys = [point.cog_x for point in points], [point.cog_y for point in points]
    xmin, xmax = min(xs), max(xs)
    ymin, ymax = min(ys), max(ys)
    x_edges = [xmin + (xmax - xmin) * i / bins for i in range(bins + 1)]
    y_edges = [ymin + (ymax - ymin) * i / bins for i in range(bins + 1)]
    cells: dict[tuple[int, int], list[float]] = defaultdict(list)
    for point in points:
        ix = min(bins - 1, int((point.cog_x - xmin) / max(xmax - xmin, 1e-12) * bins))
        iy = min(bins - 1, int((point.cog_y - ymin) / max(ymax - ymin, 1e-12) * bins))
        cells[ix, iy].append(point.cost)
    return x_edges, y_edges, cells


def plot(points: list[Point], data: NodeData, bins: int, color_by_spread: bool) -> None:
    enrich(points, data)
    xs = [point.cog_x for point in points]
    ys = [point.cog_y for point in points]
    costs = [point.cost for point in points]
    node_xmin, node_xmax = min(data.x), max(data.x)
    node_ymin, node_ymax = min(data.y), max(data.y)

    def centered_power_transform(lower: float, upper: float, exponent: float = 0.5):
        center = (lower + upper) / 2
        half_range = (upper - lower) / 2
        if half_range == 0:
            return lambda value: value, lambda value: value

        def forward(value: float) -> float:
            normalized = (value - center) / half_range
            return np.sign(normalized) * np.abs(normalized) ** exponent

        def inverse(value: float) -> float:
            return center + half_range * np.sign(value) * np.abs(value) ** (1 / exponent)

        return forward, inverse

    x_transform = centered_power_transform(node_xmin, node_xmax)
    y_transform = centered_power_transform(node_ymin, node_ymax)
    norm = colors.Normalize(min(costs), max(costs) or min(costs) + 1.0)
    figure = plt.figure(figsize=(16, 9))
    axis = figure.add_subplot(121, projection="3d")
    top = figure.add_subplot(122)
    axis.set_xscale("function", functions=x_transform)
    axis.set_yscale("function", functions=y_transform)
    top.set_xscale("function", functions=x_transform)
    top.set_yscale("function", functions=y_transform)
    waves = sorted({point.wave for point in points})
    wave_colors = {wave: cm.turbo(index / max(1, len(waves) - 1)) for index, wave in enumerate(waves)}
    for wave in waves:
        wave_points = sorted((point for point in points if point.wave == wave), key=lambda point: point.iteration)
        axis.plot([point.cog_x for point in wave_points], [point.cog_y for point in wave_points],
                  [point.cost for point in wave_points], color=wave_colors[wave], label=f"wave {wave}", alpha=0.8)
        axis.scatter([wave_points[0].cog_x], [wave_points[0].cog_y], [wave_points[0].cost],
                     color=wave_colors[wave], marker="^", s=35)
        local = min(wave_points, key=lambda point: point.cost)
        axis.scatter([local.cog_x], [local.cog_y], [local.cost], color=wave_colors[wave], s=40)
    axis.scatter(xs, ys, costs, c=[norm(cost) for cost in costs], cmap="viridis", alpha=0.28, s=12)
    best = min(points, key=lambda point: point.cost)
    axis.scatter([best.cog_x], [best.cog_y], [best.cost], marker="*", s=180, color="black", label="best-so-far")
    axis.set(xlabel="CoG x", ylabel="CoG y", zlabel="cost")
    axis.set_xlim(node_xmin, node_xmax)
    axis.set_ylim(node_ymin, node_ymax)
    axis.set_title("Solution depth map (lower cost = deeper)")
    axis.invert_zaxis()
    axis.legend(fontsize=7)

    x_edges, y_edges, cells = bin_surface(points, bins)
    floor_x, floor_y, floor_z = [], [], []
    ceiling_x, ceiling_y, ceiling_z = [], [], []
    mean_x, mean_y, mean_z = [], [], []
    for (ix, iy), values in cells.items():
        center_x = (x_edges[ix] + x_edges[ix + 1]) / 2
        center_y = (y_edges[iy] + y_edges[iy + 1]) / 2
        floor_x.append(center_x)
        floor_y.append(center_y)
        floor_z.append(min(values))
        ceiling_x.append(center_x)
        ceiling_y.append(center_y)
        ceiling_z.append(max(values))
        mean_x.append(center_x)
        mean_y.append(center_y)
        mean_z.append(sum(values) / len(values))
        top.add_patch(plt.Rectangle((x_edges[ix], y_edges[iy]),
                                     x_edges[ix + 1] - x_edges[ix], y_edges[iy + 1] - y_edges[iy],
                                     facecolor=cm.viridis(norm(min(values))), alpha=0.45))
        top.text((x_edges[ix] + x_edges[ix + 1]) / 2, (y_edges[iy] + y_edges[iy + 1]) / 2,
                 f"{min(values):.0f}/{sum(values) / len(values):.0f}/{max(values):.0f}",
                 ha="center", va="center", fontsize=5)
    axis.scatter(floor_x, floor_y, floor_z, color="navy", alpha=0.45, s=8, label="bin min floor")
    axis.scatter(ceiling_x, ceiling_y, ceiling_z, color="gray", alpha=0.12, s=8, label="bin max ceiling")
    axis.scatter(mean_x, mean_y, mean_z, color="orange", alpha=0.25, s=8, label="bin mean")
    scatter_size = [20 + 80 * point.spread / max(point.spread for point in points) if color_by_spread else 25
                    for point in points]
    top.scatter(xs, ys, c=costs, cmap="viridis", norm=norm, s=scatter_size, alpha=0.35, edgecolors="none")
    top.scatter([best.cog_x], [best.cog_y], marker="*", s=140, color="black")
    top.set(xlabel="CoG x", ylabel="CoG y", title="Top-down cost contour (fixed bounds)")
    top.set_xlim(node_xmin, node_xmax)
    top.set_ylim(node_ymin, node_ymax)
    figure.tight_layout()
    plt.show()


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("log_csv", type=Path, nargs="?")
    parser.add_argument("--bins", type=int, default=25)
    parser.add_argument("--color-by-spread", action="store_true")
    args = parser.parse_args()
    if args.log_csv is None:
        picker = Tk()
        picker.withdraw()
        picker.attributes("-topmost", True)
        log_csv_name = filedialog.askopenfilename(
            title="Select solver log CSV",
            filetypes=[("CSV files", "*.csv"), ("All files", "*.*")],
        )
        picker.destroy()
        if not log_csv_name:
            parser.error("a solver log CSV is required")
        log_csv = Path(log_csv_name)
    else:
        log_csv = args.log_csv
    node_data = dataset_path_from_log(log_csv)
    points = load_log(log_csv)
    plot(points, load_node_data(node_data), max(2, args.bins), args.color_by_spread)


if __name__ == "__main__":
    main()
