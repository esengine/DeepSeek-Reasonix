package control

// legacyResearchArchive is a read-only compatibility boundary for Goal
// sidecars and prompts that still reference an old .reasonix/autoresearch
// task. New Goal runs never create, update, list, or expose those archives.

import (
	"log/slog"
	"strings"

	"reasonix/internal/autoresearch"
	"reasonix/internal/evidence"
)

type legacyResearchSetup struct {
	// goal is the original objective recovered from task_spec.json when the
	// user named an explicit archive path. Empty when no archive was referenced.
	goal        string
	taskID      string
	blockReason string
	notice      string
	explicit    bool
}

type legacyResearchArchive struct {
	store *autoresearch.Store
}

// prepare reads an explicitly referenced legacy task. It has no create path
// and never mutates the archive, even when validation fails.
func (m legacyResearchArchive) prepare(goal string) legacyResearchSetup {
	taskID, found, parseErr := autoresearch.ExplicitTaskID(goal)
	if !found {
		return legacyResearchSetup{}
	}
	if parseErr != nil {
		return legacyResearchSetup{explicit: true, blockReason: parseErr.Error()}
	}
	if m.store == nil {
		return legacyResearchSetup{
			explicit:    true,
			taskID:      taskID,
			blockReason: "legacy research archive is unavailable for this workspace",
		}
	}
	original, err := m.loadGoalText(taskID)
	if err != nil {
		slog.Warn("controller: resume legacy autoresearch task", "err", err)
		return legacyResearchSetup{explicit: true, taskID: taskID, blockReason: err.Error()}
	}
	return legacyResearchSetup{
		goal:     original,
		taskID:   taskID,
		notice:   "legacy research archive loaded: " + taskID,
		explicit: true,
	}
}

// loadGoalText returns the original objective stored in a historical archive.
func (m legacyResearchArchive) loadGoalText(taskID string) (string, error) {
	if m.store == nil {
		return "", errLegacyArchiveUnavailable
	}
	task, err := m.store.LoadTask(taskID)
	if err != nil {
		return "", err
	}
	goal := strings.TrimSpace(task.Spec.Goal)
	if goal == "" {
		return "", errLegacyArchiveMissingGoal
	}
	return goal, nil
}

var (
	errLegacyArchiveUnavailable = errString("legacy research archive is unavailable for this workspace")
	errLegacyArchiveMissingGoal = errString("legacy research archive is missing goal text")
)

type errString string

func (e errString) Error() string { return string(e) }

func (c *Controller) prepareLegacyResearchTask(goal string) legacyResearchSetup {
	return c.legacyResearchArchive.prepare(goal)
}

func (c *Controller) restorePendingLegacyGoal(legacy legacyGoalRestore) bool {
	if legacy.taskID == "" {
		goal, epoch, ok := c.goals.legacyArchiveBlockedState()
		if ok {
			setup := c.prepareLegacyResearchTask(goal)
			if setup.explicit {
				legacy = legacyGoalRestore{taskID: setup.taskID, epoch: epoch, explicit: true}
			}
		}
	}
	// A malformed explicit archive path has no safe task id to load. Keep the
	// Controller-owned retry token so ResumeGoal cannot fall through to the
	// ordinary Goal resume path and execute the raw path text as an objective.
	if legacy.explicit && legacy.taskID == "" {
		c.replaceLegacyRestore(legacy)
		return true
	}
	if legacy.taskID == "" || strings.TrimSpace(c.goals.goalText()) != "" {
		c.replaceLegacyRestore(legacyGoalRestore{})
		return false
	}
	c.replaceLegacyRestore(legacy)
	restoreTodos := c.goalTodos()
	if len(legacy.todos) > 0 {
		restoreTodos = append([]evidence.TodoItem(nil), legacy.todos...)
		if c.executor != nil {
			c.executor.ReplaceTodoState(restoreTodos)
		}
	}
	goal, err := c.legacyResearchArchive.loadGoalText(legacy.taskID)
	if err != nil {
		if epoch, ok := c.goals.blockLegacyRestore(legacy.epoch, err.Error()); ok {
			_, _ = c.persistGoalStateAtEpoch(epoch, restoreTodos)
			c.advanceLegacyRestoreEpoch(legacy.taskID, legacy.epoch, epoch)
			c.notice("legacy research archive resume failed: " + err.Error())
		} else {
			c.clearLegacyRestore(legacy.taskID, legacy.epoch)
		}
		return true
	}
	if strings.TrimSpace(c.goals.goalText()) == "" {
		if epoch, ok := c.goals.fillGoalTextIfEmpty(legacy.epoch, goal); ok {
			_, persistErr := c.persistGoalStateAtEpoch(epoch, restoreTodos)
			if persistErr != nil {
				reason := "persist migrated legacy Goal: " + persistErr.Error()
				if blockedEpoch, blocked := c.goals.failLegacyRestorePersistence(epoch, reason); blocked {
					c.replaceLegacyRestore(legacyGoalRestore{taskID: legacy.taskID, todos: restoreTodos, epoch: blockedEpoch})
					c.notice("legacy research archive resume failed: " + reason)
				} else {
					c.clearLegacyRestore(legacy.taskID, legacy.epoch)
				}
			} else {
				c.goals.clearLegacyTaskID(epoch)
				c.clearLegacyRestore(legacy.taskID, legacy.epoch)
			}
		} else {
			c.clearLegacyRestore(legacy.taskID, legacy.epoch)
		}
	}
	return true
}

func (c *Controller) retryBlockedLegacyGoal() (handled, resumed bool) {
	goal, epoch, blocked := c.goals.legacyArchiveBlockedState()
	if !blocked {
		return false, false
	}
	legacy, hasLegacy := c.legacyRestoreSnapshot()
	if !hasLegacy || legacy.epoch != epoch || legacy.taskID == "" {
		// A blocked sidecar without a Controller-owned archive identity is a
		// fail-closed migration boundary after restart. Never resume raw text.
		return true, false
	}
	taskID := legacy.taskID
	setup := c.prepareLegacyResearchTask(goal)
	resolvedGoal, reason := setup.goal, setup.blockReason
	if !setup.explicit {
		var err error
		resolvedGoal, err = c.legacyResearchArchive.loadGoalText(taskID)
		if err != nil {
			reason = err.Error()
		}
	} else if setup.taskID != taskID {
		reason = "legacy research archive identity changed during retry"
	}
	if reason != "" || strings.TrimSpace(resolvedGoal) == "" {
		if reason == "" {
			reason = "legacy research archive could not be recovered"
		}
		if nextEpoch, applied := c.goals.blockLegacyRestore(epoch, reason); applied {
			_, _ = c.persistGoalStateAtEpoch(nextEpoch, c.goalTodos())
			c.replaceLegacyRestore(legacyGoalRestore{taskID: taskID, epoch: nextEpoch})
		}
		c.notice("legacy research archive resume failed: " + reason)
		return true, false
	}
	todos := c.goalTodos()
	resumedEpoch, applied := c.goals.resumeLegacyArchive(epoch, resolvedGoal)
	if !applied {
		c.replaceLegacyRestore(legacyGoalRestore{})
		return true, false
	}
	persisted, persistErr := c.persistGoalStateAtEpoch(resumedEpoch, todos)
	if persistErr != nil {
		reason := "persist migrated legacy Goal: " + persistErr.Error()
		if blockedEpoch, blocked := c.goals.failLegacyRestorePersistence(resumedEpoch, reason); blocked {
			c.replaceLegacyRestore(legacyGoalRestore{taskID: taskID, todos: todos, epoch: blockedEpoch})
			c.notice("legacy research archive resume failed: " + reason)
		} else {
			c.replaceLegacyRestore(legacyGoalRestore{})
		}
		return true, false
	}
	if !persisted {
		return true, false
	}
	c.goals.clearLegacyTaskID(resumedEpoch)
	c.replaceLegacyRestore(legacyGoalRestore{})
	if setup.notice != "" {
		c.notice(setup.notice)
	}
	if c.executor != nil {
		c.executor.RestoreDeliveryCheckpoint(c.goals.deliveryState())
	}
	return true, true
}
