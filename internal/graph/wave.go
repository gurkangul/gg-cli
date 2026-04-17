package graph

import (
	"context"
	"fmt"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// WaveRecord is the read-side view of a Wave node.
type WaveRecord struct {
	ID        string
	Name      string
	Goal      string
	StartDate string
	EndDate   string
	Status    string
}

// UpsertWave creates or updates a Wave node keyed on (project_id, id).
func (c *Client) UpsertWave(ctx context.Context, id, name, goal, startDate, endDate, status string) error {
	if status == "" {
		status = "active"
	}
	cypher := `
MERGE (w:Wave {id: $wave_id, project_id: $pid})
SET w.name = $name,
    w.goal = $goal,
    w.start_date = $start_date,
    w.end_date = $end_date,
    w.status = $status`
	params := map[string]any{
		"wave_id":    id,
		"name":       name,
		"goal":       goal,
		"start_date": startDate,
		"end_date":   endDate,
		"status":     status,
	}
	_, cleanup, err := c.runQuery(ctx, cypher, params)
	if err != nil {
		return fmt.Errorf("upsert wave %q: %w", id, err)
	}
	cleanup()
	return nil
}

// AssignTaskToWave writes a (Task)-[:IN_WAVE]->(Wave) edge.
// Uses task_ref as the qdrant_id of the Task node (e.g. "TASK-042").
func (c *Client) AssignTaskToWave(ctx context.Context, taskRef, waveID string) error {
	cypher := `
MATCH (t:Task {qdrant_id: $task_ref, project_id: $pid})
MATCH (w:Wave  {id: $wave_id,   project_id: $pid})
MERGE (t)-[:IN_WAVE]->(w)`
	params := map[string]any{
		"task_ref": taskRef,
		"wave_id":  waveID,
	}
	_, cleanup, err := c.runQuery(ctx, cypher, params)
	if err != nil {
		return fmt.Errorf("assign task %q to wave %q: %w", taskRef, waveID, err)
	}
	cleanup()
	return nil
}

// ListWaves returns all Wave nodes for the project, optionally filtered by status.
// statusFilter="" returns all waves; statusFilter="active" returns active waves only.
func (c *Client) ListWaves(ctx context.Context, statusFilter string) ([]WaveRecord, error) {
	var cypher string
	var params map[string]any

	if statusFilter != "" {
		cypher = `MATCH (w:Wave {project_id: $pid, status: $status}) RETURN w ORDER BY w.start_date`
		params = map[string]any{"status": statusFilter}
	} else {
		cypher = `MATCH (w:Wave {project_id: $pid}) RETURN w ORDER BY w.start_date`
		params = nil
	}

	result, cleanup, err := c.runQuery(ctx, cypher, params)
	if err != nil {
		return nil, fmt.Errorf("list waves: %w", err)
	}
	defer cleanup()

	var waves []WaveRecord
	for result.Next(ctx) {
		rec := result.Record()
		nodeVal, ok := rec.Get("w")
		if !ok {
			continue
		}
		node, ok := nodeVal.(neo4j.Node)
		if !ok {
			continue
		}
		prop := node.Props
		waves = append(waves, WaveRecord{
			ID:        strProp(prop, "id"),
			Name:      strProp(prop, "name"),
			Goal:      strProp(prop, "goal"),
			StartDate: strProp(prop, "start_date"),
			EndDate:   strProp(prop, "end_date"),
			Status:    strProp(prop, "status"),
		})
	}
	return waves, result.Err()
}

// WaveStatus returns the Wave node for the given id and the list of task qdrant_ids in it.
func (c *Client) WaveStatus(ctx context.Context, waveID string) (WaveRecord, []string, error) {
	cypher := `
MATCH (w:Wave {id: $wave_id, project_id: $pid})
OPTIONAL MATCH (t:Task {project_id: $pid})-[:IN_WAVE]->(w)
RETURN w, collect(t.qdrant_id) AS tasks`
	params := map[string]any{"wave_id": waveID}

	result, cleanup, err := c.runQuery(ctx, cypher, params)
	if err != nil {
		return WaveRecord{}, nil, fmt.Errorf("wave status: %w", err)
	}
	defer cleanup()

	if !result.Next(ctx) {
		return WaveRecord{}, nil, fmt.Errorf("wave %q not found", waveID)
	}
	rec := result.Record()

	var wave WaveRecord
	if nodeVal, ok := rec.Get("w"); ok {
		if node, ok := nodeVal.(neo4j.Node); ok {
			prop := node.Props
			wave = WaveRecord{
				ID:        strProp(prop, "id"),
				Name:      strProp(prop, "name"),
				Goal:      strProp(prop, "goal"),
				StartDate: strProp(prop, "start_date"),
				EndDate:   strProp(prop, "end_date"),
				Status:    strProp(prop, "status"),
			}
		}
	}

	var tasks []string
	if tasksVal, ok := rec.Get("tasks"); ok {
		if tasksSlice, ok := tasksVal.([]any); ok {
			for _, v := range tasksSlice {
				if s, ok := v.(string); ok && s != "" {
					tasks = append(tasks, s)
				}
			}
		}
	}
	return wave, tasks, result.Err()
}

func strProp(props map[string]any, key string) string {
	if v, ok := props[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
