# Graph viewer and solver workbench

This is an intentionally small experiment surface. It contains only:

- the JSON data model and loader in `model/`;
- data-only calculations such as statistics, distances, and solution cost in `model/`;
- a single Ebiten window that draws nodes and solver assignments;
- cached graph rendering so geometry is rebuilt only after a state or view change;
- basic node selection and solver controls.

The application separates the model, solver engine, and Ebiten renderer:

- `model/`: compact graph data and cost calculations;
- `solver/`: iterative solver interface and execution engine;
- `algorithms/`: concrete iterative optimization algorithms;
- `renderer/`: cached graph visualization synchronized with solver snapshots.

The current algorithm is the independent multi-wave metaheuristic in
`algorithms/multiWave.go`. The greedy and local-search implementations remain
available as separate algorithms.

The `model/` package is deliberately split by responsibility:

- `types.go`: JSON-backed data structures and solution/statistics result types;
- `load.go`: JSON loading and structural validation;
- `statistics.go`: traffic summaries;
- `solution.go`: distances and solution cost validation/calculation;
- `solution_io.go`: JSON solution saving and loading.

## Multi-Wave algorithm

`MultiWave` is an educational, bounded metaheuristic inspired by Fred
Glover's multi-wave idea. It is intentionally implemented as a separate
solver; it does not modify or depend on the local-search solver.

### State retained between steps

The solver keeps four important solution states:

- `current`: the solution currently being explored;
- `waveBest`: the best solution found in the current wave;
- `best`: the best solution found across all waves;
- `elite`: a small, cost-sorted archive of strong solutions from previous
  waves.

It also stores the current wave number, iteration counters, and each
solution's cached cost. Solutions placed in the archive are cloned, so future
mutations cannot change historical results.

### What one `Step` does

Each call performs exactly one neighborhood move:

1. Enumerate swaps between non-hub members in different groups.
2. Evaluate the best admissible swap.
3. Reject tabu swaps unless they improve the global best (aspiration).
4. Apply the selected move and record it in tabu memory.
5. Update `current`, `waveBest`, and global `best`.
6. Return a copy of the global best solution.

The renderer can therefore use Space to inspect one attempt at a time.
Continuous mode simply calls the same `Step` method repeatedly.

### Waves and elite memory

After `WaveIterations` moves:

1. The wave's best solution is inserted into `elite`.
2. The archive is sorted by cost and capped at `EliteSize`.
3. If `MaxWaves` has not been reached, a solution is selected from the elite
   archive.
4. That solution is perturbed to start the next wave and tabu memory is reset.

The global best solution is never replaced by a worse solution. This means
restarts can explore new regions without losing the strongest result found so
far.

### Configuration

The default configuration is:

```go
MultiWaveConfig{
    WaveIterations:    100,
    MaxWaves:          20,
    EliteSize:         5,
    PerturbationSwaps: 2,
    TabuTenure:        15,
}
```

Use `NewMultiWaveWithConfig` to tune these values. For example, smaller waves
make restarts happen more often; a larger elite archive preserves more
historical alternatives; and more perturbation swaps produce more radical
restarts.

`NewMultiWaveWithSeed` starts from a previously saved and validated solution.
This is useful for continuing an experiment without changing the local-search
implementation.

### Capacity invariant

The default capacity is five nodes per hub, including the hub itself. A dataset
can override this with a `parameters.hubCapacities` array containing one
positive capacity per hub. Capacities must sum to the node count. Multi-Wave
uses member swaps and capacity-preserving hub promotion, so different hub
capacities remain feasible throughout the search.

For example, a 12-node instance with three differently sized hubs can use:

```json
"parameters": {
  "numberOfNodes": 12,
  "numberOfHubs": 3,
  "hubCapacities": [2, 4, 6],
  "alpha": 0.5
}
```

If `hubCapacities` is omitted, capacities are derived as evenly as possible
from the node and hub counts for backward compatibility.

### Converting official FAF5 data

Download FAF5 from the official Bureau of Transportation Statistics page:

<https://faf.ornl.gov/faf5/>

For this solver, use the regional FAF5 database and the official FAF zone
shapefile linked under **FAF5 Zones** on that page. The converter
`convert_faf5.py` accepts a FAF flow CSV plus either a zone CSV or the
shapefile ZIP:

```powershell
python -m pip install pyshp
python convert_faf5.py `
  --flows path\to\FAF5_flows.csv `
  --zones path\to\2017_CFS_Metro_Areas_with_FAF.zip `
  --output data\faf5_2017.json `
  --year 2017 `
  --hubs 10 `
  --alpha 0.5
```

The flow CSV needs origin/destination FAF zone columns and a quantity column,
such as `tons_2017`. The converter aggregates commodity and mode rows into
one undirected demand value per zone pair, because the current model uses a
symmetric quantity matrix. FAF coordinates are geographic coordinates; for
large-area accuracy, project them to a planar coordinate system before using
the result for distance-sensitive comparisons.

### Inspecting progress in code

The concrete algorithm exposes `Status()` in addition to the generic
`solver.Iterative` interface:

```go
status := multiWave.Status()
fmt.Println(status.Wave, status.WaveIteration, status.BestCost)
```

The generic engine only requires `Step()`, so the algorithm remains compatible
with the existing renderer and execution controls.

From the repository root:

```powershell
go run .
```

Press `O` to choose another JSON file, `Space` to execute one solver iteration,
`Enter` to run continuously, `P` to pause, `S` to save the current solution,
`L` to load a saved solution, and `R` to reset the view. Drag to pan, use the
mouse wheel or `+`/`-` to zoom, and click a node to select it. While running,
the renderer automatically adjusts iterations per frame toward a 60 FPS target
and reserves part of each frame for rendering. `[` and `]` can still make
manual one-step adjustments.

Click **Terminate solver** (or press `T`) to stop the current run immediately
and mark its current solution as finished.

Solutions are saved as `*.solution.json` files containing the hubs, node
assignments, iteration number, completion state, and cost when the solution is
complete. Loading a solution restores it for inspection and pauses execution.

Completed runs are appended to `solver_runs.csv` with the dataset path, solver,
iteration count, final cost, and completion status.

Each Multi-Wave execution writes its own CSV under `runs\`, named with the
solver, dataset, and run ID. A row is appended whenever its best cost changes.
The header identifies the solver and dataset, and each row contains the run ID,
timestamp, wave number, wave iteration, total iteration, best cost, and the
solution as JSON. This keeps executions separate for later visualization.

To compare solver runs in a readable table, use the included Python report:

```powershell
python SolverData.py
```

This opens a graphical dashboard with score and iteration charts, summary
cards, a run table, and dropdown filters for solver and dataset. Lower scores
are better. Use `--csv path\to\solver_runs.csv` to load a different log.

To inspect one Multi-Wave run as an interactive depth map:

```powershell
python depth_map.py
```

The program opens a file picker for the solver log CSV and reads the node data
path from its `dataset` column. A CSV path can still be supplied as a
positional argument for scripted use.

The tool accepts the current `solution` JSON log column and the older
`hubs`/`assignment` columns. For every row it looks up the hub node coordinates
from the dataset and uses their unweighted arithmetic mean as `(x, y)`. The
CSV `cost` is used directly as depth; no cost is recalculated. It shows a
rotatable 3D map plus a fixed-bounds top-down binned view. Bin labels are
`min/mean/max`. Use `--color-by-spread` to scale top-down markers by hub spread.

The full model dump is intentionally opt-in because it is expensive for large
graphs:

```powershell
$env:NHUBS_PRINT_INSTANCE = "1"
go run .
```
