# internal/graph — Test Scenarios

## TestLoadAll

**Setup:** Pre-populate DB with two memories and an edge between them.

**Assertions:** After `LoadAll`, `Neighbors` returns the neighbor ID.

## TestAddEdge

**Setup:** Empty graph; add edge A→B.

**Assertions:** B in Neighbors(A); A in Neighbors(B); DB GetEdges returns the edge.

## TestNeighbors

**Setup:** A→B edge with type "relates_to"; A→C edge with type "supports".

**Assertions:** `Neighbors(A, ["relates_to"])` returns only B; unfiltered returns B and C.

## TestPPR

**Setup:** 4-node chain: A→B→C→D; seed = A.

**Assertions:** B has highest score after A; scores sum to ~1.0.

## TestAutoLink

**Setup:** DB with 3 memories with cosine > 0.75; add 4th similar memory.

**Assertions:** At least one `relates_to` edge created from 4th memory to existing ones.