import json
import numpy as np

def generate_hub_dataset(n_nodes=25, n_hubs=5, alpha=0.5, filename="generated_network.json", seed=42):
    """
    Generates a p-hub dataset JSON file matching the specified schema.
    """
    np.random.seed(seed)
    
    # 1. Generate Clustered Spatial Coordinates (Simulating metropolitan areas)
    n_clusters = max(2, n_nodes // 5)
    cluster_centers = np.random.uniform(10, 90, size=(n_clusters, 2))
    
    node_coords = np.zeros((n_nodes, 2))
    for i in range(n_nodes):
        center = cluster_centers[np.random.choice(n_clusters)]
        # Add normal noise around the cluster center
        node_coords[i] = center + np.random.normal(0, 12, size=2)
        # Clip to ensure coordinates stay within a nice [0, 100] canvas
        node_coords[i] = np.clip(node_coords[i], 0, 100)

    # Build nodes list
    nodes_list = []
    for i in range(n_nodes):
        nodes_list.append({
            "node": int(i),
            "x": float(node_coords[i, 0]),
            "y": float(node_coords[i, 1])
        })

    # 2. Compute Euclidean distance matrix for gravity model scaling
    T = np.zeros((n_nodes, n_nodes))
    for i in range(n_nodes):
        for j in range(n_nodes):
            T[i, j] = np.linalg.norm(node_coords[i] - node_coords[j])

    # Assign node "masses/weights" (representing city size/importance)
    node_weights = np.random.uniform(20, 100, size=n_nodes)

    # 3. Generate Edges (Flow quantities q using a Gravity Model)
    edges_list = []
    for i in range(n_nodes):
        for j in range(n_nodes):
            if i == j:
                q_val = 0
            else:
                # Gravity model: flow proportional to mass product, inversely to distance
                distance_friction = max(T[i, j], 1.0) # Prevent division by zero
                raw_flow = (node_weights[i] * node_weights[j]) / (distance_friction ** 1.2)
                # Scale and cast to integer demand matching typical benchmarks
                q_val = int(np.clip(raw_flow * 0.1 + np.random.randint(20, 80), 10, 75))
            
            edges_list.append({
                "fromNode": int(i),
                "toNode": int(j),
                "q": int(q_val)
            })

    # 4. Assemble Final Dictionary Structure
    dataset = {
        "parameters": {
            "numberOfNodes": int(n_nodes),
            "numberOfHubs": int(n_hubs),
            "alpha": float(alpha)
        },
        "nodes": nodes_list,
        "edges": edges_list
    }

    # 5. Save to JSON File
    with open(filename, "w", encoding="utf-8") as f:
        json.dump(dataset, f, indent=2)
        
    print(f"Dataset successfully generated and saved to '{filename}' with {n_nodes} nodes and {n_hubs} hubs.")
    return dataset

if __name__ == "__main__":
    # Example execution: Generates a 15-node, 3-hub network configuration
    generate_hub_dataset(n_nodes=100, n_hubs=20, alpha=0.5, filename="data_100_p.json")