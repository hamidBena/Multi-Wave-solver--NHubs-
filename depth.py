from concurrent.futures import ProcessPoolExecutor
import csv
import itertools
import json
import os
import time
from tkinter import filedialog
import tkinter as tk
import matplotlib.tri as mtri
import numpy as np
import plotly.graph_objects as go
from scipy.optimize import linear_sum_assignment
from scipy.spatial.distance import cdist


# Top-level worker function for parallel multiprocessing
def evaluate_config(H, N, h, node_coords, q, T, TR, base_cap, remainder):
  H_list = list(H)
  non_hubs = [i for i in range(N) if i not in H_list]

  hub_slots = []
  for i_h, k in enumerate(H_list):
    cap = base_cap + (1 if i_h < remainder else 0)
    hub_slots.extend([k] * cap)

  cost_matrix = np.zeros((len(non_hubs), len(hub_slots)))
  for r, i in enumerate(non_hubs):
    for c, k in enumerate(hub_slots):
      cost_matrix[r, c] = np.sum(q[i, :] * T[i, k] + q[:, i] * T[k, i])

  row_ind, col_ind = linear_sum_assignment(cost_matrix)

  assignments = {k: k for k in H_list}
  total_assignment_distance = 0.0
  for r, c in zip(row_ind, col_ind):
    node_i = non_hubs[r]
    assigned_hub = hub_slots[c]
    assignments[node_i] = assigned_hub
    total_assignment_distance += T[node_i, assigned_hub]

  avg_node_to_hub_dist = total_assignment_distance / len(non_hubs)

  total_cost = 0.0
  for i in range(N):
    h1 = assignments[i]
    for j in range(N):
      h2 = assignments[j]
      total_cost += q[i, j] * (T[i, h1] + TR[h1, h2] + T[h2, j])

  h_coords = node_coords[H_list]
  cog = np.mean(h_coords, axis=0)
  z_elevation = total_cost + (avg_node_to_hub_dist * 500.0)
  dispersion = np.mean(np.linalg.norm(h_coords - cog, axis=1))

  return cog[0], cog[1], z_elevation, dispersion, total_cost


if __name__ == "__main__":
  # 1. Open Windows File Explorer to select the CSV Log File
  root = tk.Tk()
  root.withdraw()

  print("Opening file explorer to select your solver CSV log file...")
  log_file_path = filedialog.askopenfilename(
      title="Select Solver CSV Log File",
      filetypes=[("CSV files", "*.csv"), ("All files", "*.*")],
  )

  if not log_file_path:
    print("No log file selected. Exiting.")
    exit()

  print(f"Loading log data from: {log_file_path}")

  # 2. Parse CSV Log & Extract JSON Dataset Path safely by header name
  log_entries = []
  data_file_path = None

  with open(log_file_path, "r", encoding="utf-8") as f:
    reader = csv.reader(f)
    header = [h.strip().lower() for h in next(reader)]

    dataset_idx = header.index("dataset") if "dataset" in header else 1
    cost_idx = header.index("cost") if "cost" in header else 7
    solution_idx = header.index("solution") if "solution" in header else 8

    for row in reader:
      if not row or len(row) <= max(dataset_idx, cost_idx, solution_idx):
        continue

      val = row[dataset_idx].strip()
      if not data_file_path and (".json" in val or "\\" in val or "/" in val):
        data_file_path = val

      try:
        sol_data = json.loads(row[solution_idx])
        log_entries.append({
            "cost": float(row[cost_idx]),
            "hubs": sol_data["hubs"],
            "assign": sol_data["assign"],
        })
      except json.JSONDecodeError:
        continue

  print(f"Extracted dataset path from log: {data_file_path}")

  # 3. Read and Parse the Network JSON File
  with open(data_file_path, "r") as f:
    data = json.load(f)

  N = data["parameters"]["numberOfNodes"]
  h = data["parameters"]["numberOfHubs"]
  alpha = data["parameters"]["alpha"]

  print(f"Network Parameters loaded -> Nodes: {N}, Hubs: {h}, Alpha: {alpha}")

  # Parse node coordinates
  node_coords = np.zeros((N, 2))
  for item in data["nodes"]:
    node_idx = item["node"]
    node_coords[node_idx] = [item["x"], item["y"]]

  # Parse edges into flow matrix q
  q = np.zeros((N, N))
  for item in data["edges"]:
    u = item["fromNode"]
    v = item["toNode"]
    q[u, v] = item["q"]

  # Distance matrices
  T = cdist(node_coords, node_coords, metric="euclidean")
  TR = alpha * T

  # 4. Enumerate Unique Hub Configurations (Parallelized Background Terrain)
  non_hub_nodes_total = N - h
  base_cap = non_hub_nodes_total // h
  remainder = non_hub_nodes_total % h

  hub_combinations = list(itertools.combinations(range(N), h))
  total_configs = len(hub_combinations)
  max_workers = os.cpu_count()

  print(
      f"Evaluating background search space ({total_configs:,} configurations)"
      f" across {max_workers} CPU cores..."
  )

  start_time = time.time()
  xs, ys, zs, ds = [], [], [], []
  best_cost = float("inf")

  with ProcessPoolExecutor(max_workers=max_workers) as executor:
    futures = [
        executor.submit(
            evaluate_config,
            H,
            N,
            h,
            node_coords,
            q,
            T,
            TR,
            base_cap,
            remainder,
        )
        for H in hub_combinations
    ]

    for future in futures:
      cx, cy, z_elev, disp, cost = future.result()
      xs.append(cx)
      ys.append(cy)
      zs.append(z_elev)
      ds.append(disp)
      if cost < best_cost:
        best_cost = cost

  print(
      "Terrain landscape generated in"
      f" {time.time() - start_time:.2f} seconds!"
  )

  # Convert background arrays
  xs, ys, zs, ds = np.array(xs), np.array(ys), np.array(zs), np.array(ds)

  # 5. Process Solver Log Coordinates & Elevations (True Elevation)
  log_xs, log_ys, log_zs = [], [], []
  for entry in log_entries:
    H_list = entry["hubs"]
    h_coords = node_coords[H_list]
    cog = np.mean(h_coords, axis=0)

    assigns = entry["assign"]
    total_dist = sum(T[i, assigns[i]] for i in range(N))
    avg_dist = total_dist / N

    z_elev = entry["cost"] + (avg_dist * 500.0)

    log_xs.append(cog[0])
    log_ys.append(cog[1])
    log_zs.append(z_elev)

  log_xs, log_ys, log_zs = np.array(log_xs), np.array(log_ys), np.array(log_zs)

  # 6. Build Subsampled Mesh and Render via Plotly (WebGL)
  print("Rendering solid terrain mesh and top-layer solver path via Plotly...")
  mesh_sample_size = min(8000, len(xs))
  indices = np.random.choice(len(xs), size=mesh_sample_size, replace=False)

  x_mesh = xs[indices]
  y_mesh = ys[indices]
  z_mesh = zs[indices]
  d_mesh = ds[indices]


  def normalize(arr):
    mn, mx = np.min(arr), np.max(arr)
    return (arr - mn) / (mx - mn + 1e-8)


  d_norm = normalize(d_mesh)
  z_norm = normalize(z_mesh)
  r_channel = d_norm
  g_channel = 1.0 - z_norm
  b_channel = 0.0 * (d_norm + z_norm)

  vertex_colors = [
      f"rgb({int(r*255)}, {int(g*255)}, {int(b*255)})"
      for r, g, b in zip(r_channel, g_channel, b_channel)
  ]

  triang = mtri.Triangulation(x_mesh, y_mesh)
  raw_triangles = triang.get_masked_triangles()

  if hasattr(raw_triangles, "mask") and raw_triangles.mask is not np.ma.nomask:
    valid_mask = ~raw_triangles.mask.any(axis=1)
    triangles = np.asarray(raw_triangles.data)[valid_mask]
  else:
    triangles = np.asarray(raw_triangles)

  triangles = triangles.astype(int)

  fig = go.Figure(
      data=[
          go.Mesh3d(
              x=x_mesh,
              y=y_mesh,
              z=z_mesh,
              i=triangles[:, 0],
              j=triangles[:, 1],
              k=triangles[:, 2],
              vertexcolor=vertex_colors,
              opacity=1.0,
              name="Terrain Mesh",
          ),
          go.Scatter3d(
              x=log_xs,
              y=log_ys,
              z=log_zs,
              mode="lines+markers",
              line=dict(color="#00FFFF", width=5),
              marker=dict(
                  size=4,
                  color="#111827",
                  line=dict(color="black", width=0.5),
              ),
              name="Solver Trajectory",
          ),
      ]
  )

  fig.update_layout(
      title=(
          f"Multi-Wave Solver Trajectory on Landscape (N={N}, h={h})<br>Final"
          f" Log Cost: {log_entries[-1]['cost']:.2f}"
      ),
      scene=dict(
          xaxis_title="Hub CoG - X Coordinate",
          yaxis_title="Hub CoG - Y Coordinate",
          zaxis_title="Penalized Cost Elevation (Z)",
          camera=dict(eye=dict(x=1.5, y=-1.5, z=1.2)),
      ),
      margin=dict(r=20, b=10, l=10, t=60),
  )

  fig.show()