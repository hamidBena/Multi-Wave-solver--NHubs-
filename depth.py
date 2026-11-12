import json
import csv
import time
import itertools
import tkinter as tk
from tkinter import filedialog
import numpy as np
from scipy.spatial.distance import cdist
from scipy.optimize import linear_sum_assignment
import matplotlib.pyplot as plt
import matplotlib.tri as mtri
from mpl_toolkits.mplot3d.art3d import Poly3DCollection

# 1. Open Windows File Explorer to select the CSV Log File
root = tk.Tk()
root.withdraw()

print("Opening file explorer to select your solver CSV log file...")
log_file_path = filedialog.askopenfilename(
    title="Select Solver CSV Log File",
    filetypes=[("CSV files", "*.csv"), ("All files", "*.*")]
)

if not log_file_path:
    print("No log file selected. Exiting.")
    exit()

print(f"Loading log data from: {log_file_path}")

# 2. Parse CSV Log & Extract JSON Dataset Path safely by header name
log_entries = []
data_file_path = None

with open(log_file_path, 'r', encoding='utf-8') as f:
    reader = csv.reader(f)
    header = [h.strip().lower() for h in next(reader)]
    
    dataset_idx = header.index('dataset') if 'dataset' in header else 1
    cost_idx = header.index('cost') if 'cost' in header else 7
    solution_idx = header.index('solution') if 'solution' in header else 8

    for row in reader:
        if not row or len(row) <= max(dataset_idx, cost_idx, solution_idx):
            continue
            
        val = row[dataset_idx].strip()
        if not data_file_path and (".json" in val or "\\" in val or "/" in val):
            data_file_path = val
            
        try:
            sol_data = json.loads(row[solution_idx])
            log_entries.append({
                'cost': float(row[cost_idx]),
                'hubs': sol_data['hubs'],
                'assign': sol_data['assign']
            })
        except json.JSONDecodeError:
            continue

print(f"Extracted dataset path from log: {data_file_path}")

# 3. Read and Parse the Network JSON File
with open(data_file_path, 'r') as f:
    data = json.load(f)

N = data['parameters']['numberOfNodes']
h = data['parameters']['numberOfHubs']
alpha = data['parameters']['alpha']

print(f"Network Parameters loaded -> Nodes: {N}, Hubs: {h}, Alpha: {alpha}")

# Parse node coordinates
node_coords = np.zeros((N, 2))
for item in data['nodes']:
    node_idx = item['node']
    node_coords[node_idx] = [item['x'], item['y']]

# Parse edges into flow matrix q
q = np.zeros((N, N))
for item in data['edges']:
    u = item['fromNode']
    v = item['toNode']
    q[u, v] = item['q']

# Distance matrices
T = cdist(node_coords, node_coords, metric='euclidean')
TR = alpha * T

# 4. Enumerate Unique Hub Configurations (Background Terrain)
non_hub_nodes_total = N - h
base_cap = non_hub_nodes_total // h
remainder = non_hub_nodes_total % h

hub_combinations = list(itertools.combinations(range(N), h))
total_configs = len(hub_combinations)
print(f"Evaluating background search space ({total_configs:,} configurations)...")

start_time = time.time()
xs, ys, zs, ds = [], [], [], []
best_cost = float('inf')

for idx, H in enumerate(hub_combinations):
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
            
    if total_cost < best_cost:
        best_cost = total_cost

    h_coords = node_coords[H_list]
    cog = np.mean(h_coords, axis=0)
    z_elevation = total_cost + (avg_node_to_hub_dist * 500.0)
    dispersion = np.mean(np.linalg.norm(h_coords - cog, axis=1))
    
    xs.append(cog[0])
    ys.append(cog[1])
    zs.append(z_elevation)
    ds.append(dispersion)

print(f"Terrain landscape generated in {time.time() - start_time:.2f} seconds!")

# Convert background arrays
xs, ys, zs, ds = np.array(xs), np.array(ys), np.array(zs), np.array(ds)

# 5. Process Solver Log Coordinates & Elevations (True Elevation)
log_xs, log_ys, log_zs = [], [], []
for entry in log_entries:
    H_list = entry['hubs']
    h_coords = node_coords[H_list]
    cog = np.mean(h_coords, axis=0)
    
    assigns = entry['assign']
    total_dist = sum(T[i, assigns[i]] for i in range(N))
    avg_dist = total_dist / N
    
    z_elev = entry['cost'] + (avg_dist * 500.0)
    
    log_xs.append(cog[0])
    log_ys.append(cog[1])
    log_zs.append(z_elev)

log_xs, log_ys, log_zs = np.array(log_xs), np.array(log_ys), np.array(log_zs)

# 6. Build Subsampled Mesh and Render
print("Rendering solid terrain mesh and top-layer solver path...")
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
rgb_points = np.column_stack((r_channel, g_channel, b_channel))

triang = mtri.Triangulation(x_mesh, y_mesh)
face_colors = np.mean(rgb_points[triang.triangles], axis=1)

triangles = triang.get_masked_triangles()
xt = triang.x[triangles]
yt = triang.y[triangles]
zt = z_mesh[triangles]
verts = np.stack((xt, yt, zt), axis=-1)

# Create Plot
fig = plt.figure(figsize=(12, 8))
ax = fig.add_subplot(projection='3d')

# Draw background terrain mesh with solid opacity
mesh_collection = Poly3DCollection(verts, facecolors=face_colors, edgecolor='k', linewidth=0.02, alpha=1.0)
ax.add_collection3d(mesh_collection)

# Plot Solver Trajectory Line and step points with explicit high zorder to always render on top
ax.plot(log_xs, log_ys, log_zs, color='#00FFFF', linewidth=2.5, alpha=1.0, zorder=10, label='Solver Trajectory')
ax.scatter(log_xs, log_ys, log_zs, color='#111827', s=30, edgecolor='black', linewidth=0.6, alpha=1.0, zorder=11, label='Solver Steps')

ax.set_xlim(xs.min(), xs.max())
ax.set_ylim(ys.min(), ys.max())
ax.set_zlim(zs.min(), zs.max())

ax.view_init(elev=35, azim=45)

ax.set_title(f'Multi-Wave Solver Trajectory on Landscape (N={N}, h={h})\nFinal Log Cost: {log_entries[-1]["cost"]:.2f}', fontsize=13, fontweight='bold')
ax.set_xlabel('Hub CoG - X Coordinate', labelpad=10)
ax.set_ylabel('Hub CoG - Y Coordinate', labelpad=10)
ax.set_zlabel('Penalized Cost Elevation ($Z$)', labelpad=10)
ax.legend(loc='upper left')

plt.tight_layout()
plt.show()