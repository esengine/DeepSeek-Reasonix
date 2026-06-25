package controllers

import controlgraph "reasonix/internal/controlplane/control_graph"

func DefaultGraph() controlgraph.ControlGraph {
	return controlgraph.ControlGraph{
		Nodes: BuiltInNodes(),
		Edges: []controlgraph.ControlEdge{
			{From: "stability-controller", To: "exploration-controller", Influence: controlgraph.InfluenceSuppress},
			{From: "semantic-drift-controller", To: "exploration-controller", Influence: controlgraph.InfluenceSuppress},
			{From: "stability-controller", To: "mutation-controller", Influence: controlgraph.InfluenceSuppress},
			{From: "exploration-controller", To: "mutation-controller", Influence: controlgraph.InfluenceAmplify},
			{From: "mutation-controller", To: "stability-controller", Influence: controlgraph.InfluenceBalance},
		},
	}
}
