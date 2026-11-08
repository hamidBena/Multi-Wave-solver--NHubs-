package model

func (i *Instance) PrintInstance() {
	println("Instance:")
	println("Number of nodes:", i.Parameters.NodeCount)
	println("Number of hubs:", i.Parameters.HubCount)
	println("Alpha:", i.Parameters.Alpha)
	println("Nodes:")
	for index, node := range i.Nodes {
		println("ID:", index, "X:", node.X, "Y:", node.Y)
	}
	println("Quantities:")
	for from := 0; from < i.Quantity.Size; from++ {
		for to := from + 1; to < i.Quantity.Size; to++ {
			println("Q[", from, "][", to, "] =", i.Quantity.At(from, to))
		}
	}
}
