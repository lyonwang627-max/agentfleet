package api

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/lyonwang/agentfleet/internal/model"
)

// 任务依赖 DAG（I20）：创建时环检测 + 依赖图查询。
// 环检测用 DFS 三色标记——白（未访问）/灰（在栈）/黑（已完成），
// 回边指向灰色节点即成环。节点规模个人级（百），递归 DFS 足够。

// detectCycle DFS 检测：从节点出发沿 depends_on 是否能回到 start。
func detectCycle(edges map[int64][]int64, start int64) bool {
	visited := map[int64]bool{}
	var dfs func(int64) bool
	dfs = func(n int64) bool {
		if n == start {
			return true
		}
		if visited[n] {
			return false
		}
		visited[n] = true
		for _, next := range edges[n] {
			if dfs(next) {
				return true
			}
		}
		return false
	}
	for _, dep := range edges[start] {
		visited = map[int64]bool{}
		if dfs(dep) {
			return true
		}
	}
	return false
}

// buildDependencyEdges 从一批任务构造 depends_on 邻接表。
func buildDependencyEdges(tasks []model.Task) map[int64][]int64 {
	edges := map[int64][]int64{}
	for _, t := range tasks {
		edges[t.ID] = t.DependsOn
	}
	return edges
}

// validateNoCycle 校验「新任务的依赖集合」是否成环；成环返回环路径描述。
func validateNoCycle(tasks []model.Task, taskID int64, dependsOn []int64) error {
	edges := buildDependencyEdges(tasks)
	edges[taskID] = dependsOn
	// 从新任务出发 DFS
	visited := map[int64]bool{}
	var dfs func(int64) bool
	dfs = func(n int64) bool {
		if n == taskID {
			return true // 回到新任务自身 → 成环
		}
		if visited[n] {
			return false
		}
		visited[n] = true
		for _, next := range edges[n] {
			if dfs(next) {
				return true
			}
		}
		return false
	}
	for _, dep := range dependsOn {
		if dep == taskID {
			return fmt.Errorf("依赖成环: 任务 %d 依赖自身", taskID)
		}
		if dfs(dep) {
			return fmt.Errorf("依赖成环：经由任务 %d 回到自身", dep)
		}
	}
	return nil
}

// dagResponse DAG 查询响应。
type dagResponse struct {
	Nodes []dagNode `json:"nodes"`
	Edges []dagEdge `json:"edges"`
	Cycle bool      `json:"cycle"`
}
type dagNode struct {
	ID     int64  `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}
type dagEdge struct {
	From int64 `json:"from"`
	To   int64 `json:"to"`
}

// taskDAG 返回任务依赖子图（含传递依赖）。
func (s *Server) taskDAG(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	tasks, _, err := s.Store.ListTasks(r.Context(), nil, 0, 1, 500)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	byID := map[int64]model.Task{}
	for _, t := range tasks {
		byID[t.ID] = t
	}
	if _, ok := byID[id]; !ok {
		writeErr(w, http.StatusNotFound, errors.New("任务不存在"))
		return
	}

	nodes := []dagNode{}
	edges := []dagEdge{}
	seen := map[int64]bool{}
	var walk func(int64)
	walk = func(n int64) {
		if seen[n] {
			return
		}
		seen[n] = true
		t, ok := byID[n]
		if !ok {
			return
		}
		nodes = append(nodes, dagNode{ID: n, Title: t.Title, Status: string(t.Status)})
		for _, dep := range t.DependsOn {
			if byID[dep].ID != 0 || byID[dep].Title != "" {
				edges = append(edges, dagEdge{From: n, To: dep})
				walk(dep)
			}
		}
	}
	walk(id)

	// 环检测：从该任务沿 deps DFS 能否回到自身（全图边集上跑，含传递依赖）
	hasCycle := detectCycle(buildDependencyEdges(tasks), id)
	writeJSON(w, http.StatusOK, dagResponse{Nodes: nodes, Edges: dedupEdges(edges), Cycle: hasCycle})
}

func dedupEdges(in []dagEdge) []dagEdge {
	seen := map[dagEdge]bool{}
	out := []dagEdge{}
	for _, e := range in {
		if !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	return out
}
