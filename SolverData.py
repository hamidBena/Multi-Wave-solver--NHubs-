"""Tkinter dashboard for comparing solver runs.

Run with:
    python SolverData.py
    python SolverData.py --csv path\\to\\solver_runs.csv
"""

from __future__ import annotations

import argparse
import csv
import tkinter as tk
from collections import defaultdict
from dataclasses import dataclass
from pathlib import Path
from tkinter import filedialog, messagebox, ttk


@dataclass(frozen=True)
class Run:
    timestamp: str
    dataset: str
    solver: str
    iterations: int
    cost: float
    finished: bool


def load_runs(path: Path) -> list[Run]:
    try:
        with path.open(newline="", encoding="utf-8") as file:
            rows = csv.DictReader(file)
            if rows.fieldnames and len(rows.fieldnames) == 1 and "," in rows.fieldnames[0]:
                rows.fieldnames = next(csv.reader([rows.fieldnames[0].strip('"')]))
            required = {"timestamp", "dataset", "solver", "iterations", "cost", "finished"}
            if not rows.fieldnames or not required.issubset(rows.fieldnames):
                raise ValueError(f"CSV must contain columns: {', '.join(sorted(required))}")

            runs = []
            for line_number, row in enumerate(rows, start=2):
                try:
                    runs.append(
                        Run(
                            timestamp=row["timestamp"],
                            dataset=Path(row["dataset"]).name,
                            solver=row["solver"],
                            iterations=int(row["iterations"]),
                            cost=float(row["cost"]),
                            finished=row["finished"].strip().lower() == "true",
                        )
                    )
                except (TypeError, ValueError) as error:
                    raise ValueError(f"invalid data on CSV line {line_number}: {error}") from error
    except FileNotFoundError as error:
        raise ValueError(f"CSV file not found: {path}") from error
    if not runs:
        raise ValueError(f"No solver runs found in {path}")
    return runs


class SolverDashboard(tk.Tk):
    """Interactive solver comparison dashboard built with the standard library."""

    COLORS = ["#4f8cff", "#ff8a4f", "#45c486", "#b06cff", "#f0bf4c", "#e85d75"]

    def __init__(self, runs: list[Run]) -> None:
        super().__init__()
        self.runs = runs
        self.title("Solver Runs Dashboard")
        self.geometry("1250x820")
        self.minsize(950, 650)
        self.configure(bg="#f4f6fb")
        self._setup_style()
        self._build_ui()
        self._refresh()

    def _setup_style(self) -> None:
        style = ttk.Style(self)
        style.theme_use("clam")
        style.configure("TFrame", background="#f4f6fb")
        style.configure("TLabel", background="#f4f6fb", foreground="#172033")
        style.configure("Title.TLabel", font=("Segoe UI", 22, "bold"), background="#f4f6fb")
        style.configure("Subtitle.TLabel", foreground="#68738a", background="#f4f6fb")
        style.configure("Card.TFrame", background="white", relief="flat")
        style.configure("CardTitle.TLabel", background="white", foreground="#68738a")
        style.configure("CardValue.TLabel", background="white", foreground="#172033",
                        font=("Segoe UI", 16, "bold"))
        style.configure("Treeview", rowheight=28, font=("Segoe UI", 10))
        style.configure("Treeview.Heading", font=("Segoe UI", 10, "bold"))

    def _build_ui(self) -> None:
        header = ttk.Frame(self, padding=(28, 22, 28, 10))
        header.pack(fill="x")
        ttk.Label(header, text="Solver Runs Dashboard", style="Title.TLabel").pack(anchor="w")
        ttk.Label(
            header,
            text="Compare scores, iterations, and solver performance at a glance",
            style="Subtitle.TLabel",
        ).pack(anchor="w", pady=(3, 0))

        controls = ttk.Frame(self, padding=(28, 8))
        controls.pack(fill="x")
        ttk.Label(controls, text="Solver:").pack(side="left")
        self.solver_var = tk.StringVar(value="All solvers")
        self.solver_box = ttk.Combobox(controls, textvariable=self.solver_var, state="readonly", width=18)
        self.solver_box.pack(side="left", padx=(7, 18))
        ttk.Label(controls, text="Dataset:").pack(side="left")
        self.dataset_var = tk.StringVar(value="All datasets")
        self.dataset_box = ttk.Combobox(controls, textvariable=self.dataset_var, state="readonly", width=18)
        self.dataset_box.pack(side="left", padx=7)
        self.solver_box.bind("<<ComboboxSelected>>", lambda _: self._refresh())
        self.dataset_box.bind("<<ComboboxSelected>>", lambda _: self._refresh())

        self.cards = ttk.Frame(self, padding=(28, 8))
        self.cards.pack(fill="x")
        self.chart_frame = ttk.Frame(self, padding=(28, 8))
        self.chart_frame.pack(fill="both", expand=True)
        self.table_frame = ttk.Frame(self, padding=(28, 8))
        self.table_frame.pack(fill="both", expand=True)

    def _filtered(self) -> list[Run]:
        solver = self.solver_var.get()
        dataset = self.dataset_var.get()
        return [
            run for run in self.runs
            if (solver == "All solvers" or run.solver == solver)
            and (dataset == "All datasets" or run.dataset == dataset)
        ]

    def _refresh(self) -> None:
        filtered = self._filtered()
        for child in self.cards.winfo_children():
            child.destroy()
        for child in self.chart_frame.winfo_children():
            child.destroy()
        for child in self.table_frame.winfo_children():
            child.destroy()

        if not filtered:
            ttk.Label(self.cards, text="No runs match the selected filters.").pack(anchor="w")
            return

        best = min(filtered, key=lambda run: run.cost)
        cards = [
            ("Runs", f"{len(filtered):,}"),
            ("Best score", f"{best.cost:,.2f}"),
            ("Average score", f"{sum(run.cost for run in filtered) / len(filtered):,.2f}"),
            ("Average iterations", f"{sum(run.iterations for run in filtered) / len(filtered):,.1f}"),
        ]
        for title, value in cards:
            card = ttk.Frame(self.cards, style="Card.TFrame", padding=(18, 12))
            card.pack(side="left", fill="x", expand=True, padx=(0, 10))
            ttk.Label(card, text=title, style="CardTitle.TLabel").pack(anchor="w")
            ttk.Label(card, text=value, style="CardValue.TLabel").pack(anchor="w", pady=(4, 0))

        self._draw_charts(filtered)
        self._draw_table(filtered)

    def _draw_charts(self, runs: list[Run]) -> None:
        score_canvas = tk.Canvas(self.chart_frame, bg="white", height=270, highlightthickness=0)
        score_canvas.pack(side="left", fill="both", expand=True, padx=(0, 8))
        iteration_canvas = tk.Canvas(self.chart_frame, bg="white", height=270, highlightthickness=0)
        iteration_canvas.pack(side="left", fill="both", expand=True, padx=(8, 0))
        self._draw_bar_chart(score_canvas, runs, "Best score by solver", "cost")
        self._draw_bar_chart(iteration_canvas, runs, "Average iterations by solver", "iterations")

    def _draw_bar_chart(self, canvas: tk.Canvas, runs: list[Run], title: str, metric: str) -> None:
        groups: dict[str, list[Run]] = defaultdict(list)
        for run in runs:
            groups[run.solver].append(run)
        values = {
            solver: min(run.cost for run in group) if metric == "cost"
            else sum(run.iterations for run in group) / len(group)
            for solver, group in groups.items()
        }
        canvas.update_idletasks()
        width = max(canvas.winfo_width(), 420)
        height = 270
        canvas.create_text(18, 18, text=title, anchor="w", font=("Segoe UI", 11, "bold"), fill="#172033")
        max_value = max(values.values()) or 1
        left, right, top, bottom = 55, width - 25, 45, height - 45
        canvas.create_line(left, bottom, right, bottom, fill="#ccd3e0")
        bar_width = max(28, (right - left) / max(len(values) * 2, 1))
        gap = (right - left) / max(len(values), 1)
        for index, (solver, value) in enumerate(sorted(values.items())):
            x = left + gap * index + (gap - bar_width) / 2
            y = bottom - (bottom - top) * value / max_value
            color = self.COLORS[index % len(self.COLORS)]
            canvas.create_rectangle(x, y, x + bar_width, bottom, fill=color, outline="")
            canvas.create_text(x + bar_width / 2, y - 8, text=f"{value:,.0f}", fill="#172033")
            canvas.create_text(x + bar_width / 2, bottom + 16, text=solver, fill="#68738a")

    def _draw_table(self, runs: list[Run]) -> None:
        columns = ("dataset", "solver", "iterations", "score", "finished", "timestamp")
        tree = ttk.Treeview(self.table_frame, columns=columns, show="headings", height=8)
        headings = {
            "dataset": "Dataset", "solver": "Solver", "iterations": "Iterations",
            "score": "Score", "finished": "Finished", "timestamp": "Timestamp",
        }
        for column in columns:
            tree.heading(column, text=headings[column])
            tree.column(column, anchor="center", width=150)
        for run in sorted(runs, key=lambda item: item.cost):
            tree.insert("", "end", values=(
                run.dataset, run.solver, f"{run.iterations:,}", f"{run.cost:,.2f}",
                "Yes" if run.finished else "No", run.timestamp.replace("T", " ")[:19],
            ))
        scrollbar = ttk.Scrollbar(self.table_frame, orient="vertical", command=tree.yview)
        tree.configure(yscrollcommand=scrollbar.set)
        tree.pack(side="left", fill="both", expand=True)
        scrollbar.pack(side="right", fill="y")


def main() -> None:
    parser = argparse.ArgumentParser(description="Open the solver runs dashboard.")
    parser.add_argument("--csv", type=Path, help="solver runs CSV file")
    args = parser.parse_args()
    if args.csv is None:
        picker = tk.Tk()
        picker.withdraw()
        csv_name = filedialog.askopenfilename(
            parent=picker,
            title="Select solver runs CSV",
            filetypes=[("CSV files", "*.csv"), ("All files", "*.*")],
        )
        picker.destroy()
        if not csv_name:
            return
        csv_path = Path(csv_name)
    else:
        csv_path = args.csv
    try:
        runs = load_runs(csv_path)
    except ValueError as error:
        root = tk.Tk()
        root.withdraw()
        messagebox.showerror("Could not load solver runs", str(error))
        root.destroy()
        return
    app = SolverDashboard(runs)
    app.solver_box["values"] = ["All solvers", *sorted({run.solver for run in runs})]
    app.dataset_box["values"] = ["All datasets", *sorted({run.dataset for run in runs})]
    app.mainloop()


if __name__ == "__main__":
    main()
