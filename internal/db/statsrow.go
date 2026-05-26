package db

type StatsRow struct {
	TotalMemories int
	ByLayer       map[int]int
	TotalEdges    int
}
