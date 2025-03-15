package dag

import (
	"fmt"
	"gonum.org/v1/gonum/graph/encoding"
	"gonum.org/v1/gonum/graph/encoding/dot"
	"gonum.org/v1/gonum/graph/simple"
)

const (
	dotNodeStyle = "filled"
)

// Node is a simple implementation of a graph node that
// includes an integer ID and a label for display.
type Node struct {
	id     int64
	Symbol string
	Weight float64
}

// ID returns the unique identifier of the node.
func (n *Node) ID() int64 {
	return n.id
}

// Attributes implements the encoding.Attributer interface.
func (n *Node) Attributes() []encoding.Attribute {
	var leaf bool
	if n.Weight > 0 {
		leaf = true
	}

	label := n.Symbol
	fillcolor := "0 0 1"
	if leaf {
		label += fmt.Sprintf("\n%.1f%%", n.Weight*100)
		fillcolor = fmt.Sprintf("0 %.1f 0.9", n.Weight)
	}
	return []encoding.Attribute{
		{Key: "label", Value: label}, // Symbol for the node
		{Key: "style", Value: dotNodeStyle},
		{Key: "fillcolor", Value: fillcolor},
		{Key: "fontsize", Value: fmt.Sprintf("%.3f", 12+(n.Weight*100))},
		{Key: "width", Value: fmt.Sprintf("%.3f", n.Weight*5)},
		{Key: "height", Value: fmt.Sprintf("%.3f", n.Weight*5)},
	}
}

// DAG wraps Gonum's directed graph and provides methods to
// add nodes and edges, as well as export to DOT format.
type DAG struct {
	*simple.DirectedGraph
	nodes map[int64]*Node
}

// NewDAG creates a new DAG.
func NewDAG() *DAG {
	return &DAG{
		DirectedGraph: simple.NewDirectedGraph(),
		nodes:         make(map[int64]*Node),
	}
}

// AddCustomNode adds a node to the DAG and returns its ID.
// func (dag *DAG) AddCustomNode(id int64, symbol string, weight float64) {
func (dag *DAG) AddCustomNode(id int64, symbol string, weight float64) {
	node := &Node{id: id, Symbol: symbol, Weight: weight}
	dag.nodes[id] = node
	dag.AddNode(node)
}

// AddCustomEdge adds a directed edge between two nodes.
func (dag *DAG) AddCustomEdge(fromID, toID int64) error {
	from := dag.nodes[fromID]
	to := dag.nodes[toID]
	if from == nil || to == nil {
		return fmt.Errorf("either from or to node does not exist")
	}
	dag.SetEdge(dag.NewEdge(from, to))

	return nil
}

// DOT returns a DOT representation of the DAG.
func (dag *DAG) DOT() (string, error) {
	data, err := dot.Marshal(dag, "DAG", "", "  ")
	if err != nil {
		return "", err
	}

	return string(data), nil
}
