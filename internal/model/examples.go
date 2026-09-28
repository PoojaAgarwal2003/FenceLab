package model

func Examples() map[string]Scenario {
	barrier := Example("barrier")
	eager := Example("eager")
	eager.Name = "Unsafe dispatch before fence acknowledgment"
	partition := Example("barrier")
	partition.Name = "Partition and heal the fence acknowledgment link"
	partition.Partitions = []Partition{{From: "store", To: "authority", StartMS: 0, EndMS: 40}}
	lost := Example("barrier")
	lost.Name = "Lost result and duplicate writes"
	lost.Faults = []Fault{
		{Kind: "result", Token: 1, Action: "drop"},
		{Kind: "write", Token: 2, Action: "duplicate", DelayMS: 0},
	}
	return map[string]Scenario{"barrier": barrier, "eager": eager, "partition": partition, "lost-result": lost}
}
