"""Convert FAF5 origin-destination data to the NHubs JSON format.

Examples:
    python convert_faf5.py --flows FAF5.7.1.csv --zones faf_zones.zip \
        --output data/faf5_2017.json --year 2017 --hubs 10 --alpha 0.5

The flow file must contain origin and destination FAF zone columns and a
quantity column such as ``tons_2017``. The zone file may be a CSV containing
zone/x/y columns or the official FAF shapefile ZIP. Shapefile support requires
the ``pyshp`` package: ``python -m pip install pyshp``.
"""

from __future__ import annotations

import argparse
import csv
import json
import zipfile
from collections import defaultdict
from pathlib import Path
from typing import Iterable


def normalized(value: str) -> str:
    return "".join(character for character in value.lower() if character.isalnum())


def find_column(columns: Iterable[str], candidates: Iterable[str]) -> str:
    normalized_columns = {normalized(column): column for column in columns}
    for candidate in candidates:
        if normalized(candidate) in normalized_columns:
            return normalized_columns[normalized(candidate)]
    raise ValueError(
        f"could not find any of {', '.join(candidates)}; available columns: "
        f"{', '.join(columns)}"
    )


def read_zones(path: Path) -> dict[int, tuple[float, float]]:
    if not path.exists():
        raise ValueError(
            f"zone file does not exist: {path}. Replace the example path with "
            "the actual downloaded FAF zone file."
        )
    if path.suffix.lower() == ".csv":
        with path.open(newline="", encoding="utf-8-sig") as file:
            reader = csv.DictReader(file)
            if not reader.fieldnames:
                raise ValueError(f"zone CSV has no header: {path}")
            zone_column = find_column(
                reader.fieldnames, ("dms_zone", "fafzone", "statefp", "zone", "id")
            )
            x_column = find_column(reader.fieldnames, ("x", "longitude", "lon", "centroid_x"))
            y_column = find_column(reader.fieldnames, ("y", "latitude", "lat", "centroid_y"))
            zones = {
                int(row[zone_column]): (float(row[x_column]), float(row[y_column]))
                for row in reader
            }
    else:
        try:
            import shapefile
        except ImportError as error:
            raise ValueError(
                "reading a shapefile requires pyshp; install it with "
                "'python -m pip install pyshp'"
            ) from error
        if not (path.is_dir() or zipfile.is_zipfile(path)):
            raise ValueError(f"zone file must be a CSV, shapefile, or shapefile ZIP: {path}")
        reader = shapefile.Reader(str(path))
        fields = [field[0] for field in reader.fields[1:]]
        zone_column = find_column(
            fields, ("dms_zone", "fafzone", "statefp", "zone", "id")
        )
        zone_index = fields.index(zone_column)
        zones = {}
        for shape_record in reader.iterShapeRecords():
            record = shape_record.record
            bounds = shape_record.shape.bbox
            zones[int(record[zone_index])] = (
                (bounds[0] + bounds[2]) / 2,
                (bounds[1] + bounds[3]) / 2,
            )
    if not zones:
        raise ValueError(f"zone file contains no zones: {path}")
    return zones


def convert(
    flows_path: Path,
    zones_path: Path,
    output_path: Path,
    year: int,
    hub_count: int,
    alpha: float,
    quantity_column: str | None,
) -> None:
    if not flows_path.is_file():
        raise ValueError(
            f"flow file does not exist: {flows_path}. Replace the example path "
            "with the actual downloaded FAF flow CSV."
        )
    zones = read_zones(zones_path)
    with flows_path.open(newline="", encoding="utf-8-sig") as file:
        reader = csv.DictReader(file)
        if not reader.fieldnames:
            raise ValueError(f"flow CSV has no header: {flows_path}")
        origin_column = find_column(
            reader.fieldnames, ("dms_orig", "dms_origst", "origin", "origin_zone")
        )
        destination_column = find_column(
            reader.fieldnames, ("dms_dest", "dms_destst", "destination", "destination_zone")
        )
        if quantity_column is None:
            quantity_column = find_column(
                reader.fieldnames,
                (f"tons_{year}", f"tons{year}", f"tonnage_{year}", "tons", "quantity"),
            )
        elif quantity_column not in reader.fieldnames:
            raise ValueError(f"quantity column does not exist: {quantity_column}")

        flow: defaultdict[tuple[int, int], float] = defaultdict(float)
        used_zones: set[int] = set()
        for line, row in enumerate(reader, start=2):
            try:
                origin = int(row[origin_column])
                destination = int(row[destination_column])
                quantity = float(row[quantity_column] or 0)
            except (KeyError, TypeError, ValueError) as error:
                raise ValueError(f"invalid flow on CSV line {line}: {error}") from error
            if quantity < 0:
                raise ValueError(f"negative quantity on CSV line {line}")
            if origin not in zones or destination not in zones:
                raise ValueError(
                    f"flow on CSV line {line} references a zone missing from {zones_path}"
                )
            if origin == destination or quantity == 0:
                used_zones.update((origin, destination))
                continue
            pair = tuple(sorted((origin, destination)))
            flow[pair] += quantity
            used_zones.update((origin, destination))

    ordered_zones = sorted(used_zones)
    if not ordered_zones:
        raise ValueError("the flow file produced no usable zones")
    if hub_count < 1 or hub_count > len(ordered_zones):
        raise ValueError(f"hub count must be between 1 and {len(ordered_zones)}")

    index = {zone: position for position, zone in enumerate(ordered_zones)}
    edges = [
        {"fromNode": index[origin], "toNode": index[destination], "q": int(round(quantity))}
        for (origin, destination), quantity in sorted(flow.items())
        if int(round(quantity)) > 0
    ]
    coordinates = [zones[zone] for zone in ordered_zones]
    capacities = [len(ordered_zones) // hub_count] * hub_count
    for slot in range(len(ordered_zones) % hub_count):
        capacities[slot] += 1
    result = {
        "parameters": {
            "numberOfNodes": len(ordered_zones),
            "numberOfHubs": hub_count,
            "hubCapacities": capacities,
            "alpha": alpha,
            "source": f"FAF5 {year}",
        },
        "nodes": [
            {"node": position, "x": x, "y": y}
            for position, (x, y) in enumerate(coordinates)
        ],
        "edges": edges,
    }
    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(json.dumps(result, indent=2) + "\n", encoding="utf-8")


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--flows", type=Path, required=True, help="downloaded FAF5 flow CSV")
    parser.add_argument("--zones", type=Path, required=True, help="zone CSV or official shapefile ZIP")
    parser.add_argument("--output", type=Path, required=True, help="NHubs JSON output path")
    parser.add_argument("--year", type=int, default=2017)
    parser.add_argument("--hubs", type=int, required=True)
    parser.add_argument("--alpha", type=float, default=0.5)
    parser.add_argument("--quantity-column")
    args = parser.parse_args()
    convert(
        args.flows,
        args.zones,
        args.output,
        args.year,
        args.hubs,
        args.alpha,
        args.quantity_column,
    )


if __name__ == "__main__":
    main()
