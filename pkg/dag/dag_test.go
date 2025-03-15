package dag_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	. "github.com/maxgio92/utrace/pkg/dag"
)

func TestAddCustomNode(t *testing.T) {
	dag := NewDAG()
	id := int64(1)
	dag.AddCustomNode(id, "main.foo", 0.20)

	node, ok := dag.Node(id).(*Node)
	if !ok {
		t.Fatal()
	}

	assert.NotNil(t, node)
	assert.Equal(t, id, node.ID())
	assert.Equal(t, 0.20, node.Weight)
	assert.Equal(t, "main.foo", node.Symbol)
}

func TestAddCustomEdge(t *testing.T) {
	dag := NewDAG()
	id1 := int64(1)
	id2 := int64(2)
	dag.AddCustomNode(id1, "main.foo", 0.20)
	dag.AddCustomNode(id2, "main.bar", 0.30)

	if err := dag.AddCustomEdge(id1, id2); err != nil {
		t.Fatal(err)
	}

	if !dag.HasEdgeFromTo(id1, id2) {
		t.Fatal()
	}
}
