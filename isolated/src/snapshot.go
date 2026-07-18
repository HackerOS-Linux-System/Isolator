package src

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

// SnapshotRecord describes one saved container state.
type SnapshotRecord struct {
	Container string
	Image     string
	CreatedAt time.Time
}

func snapshotsFile() string {
	return ConfigPath("snapshots.hk")
}

// Stored as:
//
//	[snapshots]
//	-> fedora@1731000000
//	--> container  => fedora
//	--> image      => isolator-snapshot/fedora:1731000000
//	--> created_at => 1731000000
func loadSnapshots() []SnapshotRecord {
	path := snapshotsFile()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	doc, err := LoadHKFile(path)
	if err != nil {
		return nil
	}
	sec := doc.Section("snapshots")
	var recs []SnapshotRecord
	for _, key := range sec.Keys() {
		v, _ := sec.Get(key)
		if v.Kind != HkMapKind {
			continue
		}
		m := v.MapVal
		var createdAt time.Time
		if cv, ok := m.Get("created_at"); ok {
			if n, err := cv.AsNumber(); err == nil {
				createdAt = time.Unix(int64(n), 0)
			}
		}
		recs = append(recs, SnapshotRecord{
			Container: hkGetString(m, "container", ""),
			Image:     hkGetString(m, "image", ""),
			CreatedAt: createdAt,
		})
	}
	return recs
}

func saveSnapshots(recs []SnapshotRecord) error {
	doc := NewHkDocument()
	sec := doc.Section("snapshots")
	for _, r := range recs {
		m := NewHkMap()
		m.Set("container", hkStr(r.Container))
		m.Set("image", hkStr(r.Image))
		m.Set("created_at", hkNum(float64(r.CreatedAt.Unix())))
		key := r.Container + "@" + strconv.FormatInt(r.CreatedAt.Unix(), 10)
		sec.Set(key, HkValue{Kind: HkMapKind, MapVal: m})
	}
	return WriteHKFile(snapshotsFile(), doc)
}

func latestSnapshotFor(cont string, recs []SnapshotRecord) *SnapshotRecord {
	var latest *SnapshotRecord
	for i := range recs {
		if recs[i].Container == cont {
			if latest == nil || recs[i].CreatedAt.After(latest.CreatedAt) {
				latest = &recs[i]
			}
		}
	}
	return latest
}

// HandleSnapshot commits the current state of a managed container to a
// local tagged image, so a failed `upgrade`/`update` can be undone.
func HandleSnapshot(cont string, dryRun bool) {
	if !ContainerExists(cont) {
		PrintError(fmt.Sprintf("Container '%s' not found", cont))
		return
	}
	if dryRun {
		tag := fmt.Sprintf("isolator-snapshot/%s:<timestamp>", cont)
		PrintInfo(fmt.Sprintf("[dry-run] Would commit '%s' to image '%s' and record it", cont, tag))
		return
	}
	if _, err := snapshotOne(cont); err != nil {
		PrintError(err.Error())
		return
	}
}

// snapshotOne does the actual commit + bookkeeping for a single container,
// shared by HandleSnapshot and HandleSnapshotAll.
func snapshotOne(cont string) (string, error) {
	tag := fmt.Sprintf("isolator-snapshot/%s:%d", cont, time.Now().Unix())
	PrintStep("Committing snapshot of " + cont + "...")
	if !ExecCommand(podmanBin, []string{"commit", cont, tag}) {
		return "", fmt.Errorf("snapshot of '%s' failed", cont)
	}
	recs := loadSnapshots()
	recs = append(recs, SnapshotRecord{Container: cont, Image: tag, CreatedAt: time.Now()})
	if err := saveSnapshots(recs); err != nil {
		return "", fmt.Errorf("snapshot of '%s' created but failed to record metadata: %w", cont, err)
	}
	PrintSuccess("Snapshot saved: " + tag)
	return tag, nil
}

// HandleSnapshotAll snapshots every container Isolator manages in one go —
// the natural "before I upgrade/rollback everything" preparation step for a
// real system-wide rollback later.
func HandleSnapshotAll(dryRun bool) {
	conts := GetOurContainers()
	if len(conts) == 0 {
		PrintInfo("No managed containers to snapshot")
		return
	}
	if dryRun {
		PrintInfo(fmt.Sprintf("[dry-run] Would snapshot %d container(s):", len(conts)))
		for _, c := range conts {
			fmt.Println("  " + c)
		}
		PrintInfo("[dry-run] No changes made")
		return
	}
	PrintInfo(fmt.Sprintf("Snapshotting %d managed container(s)...", len(conts)))
	failed := 0
	for _, c := range conts {
		if _, err := snapshotOne(c); err != nil {
			PrintError(err.Error())
			failed++
		}
	}
	if failed == 0 {
		PrintSuccess(fmt.Sprintf("All %d container(s) snapshotted", len(conts)))
	} else {
		PrintWarn(fmt.Sprintf("%d/%d container(s) failed to snapshot", failed, len(conts)))
	}
}

// HandleRollback restores the most recent snapshot for cont: stops and
// removes the running container, then re-creates it from the snapshot
// image, preserving the original home-directory mount.
func HandleRollback(cont string, dryRun bool) {
	recs := loadSnapshots()
	latest := latestSnapshotFor(cont, recs)
	if latest == nil {
		PrintError(fmt.Sprintf("No snapshot found for '%s' — run 'isolator snapshot %s' first, next time", cont, cont))
		return
	}
	if dryRun {
		PrintInfo(fmt.Sprintf("[dry-run] Would roll back '%s' to snapshot from %s (image: %s)", cont, latest.CreatedAt.Format(time.RFC3339), latest.Image))
		fmt.Println("  - stop + remove current container: " + cont)
		fmt.Println("  - recreate from: " + latest.Image)
		PrintInfo("[dry-run] No changes made")
		return
	}
	if err := rollbackOne(cont, latest); err != nil {
		PrintError(err.Error())
		return
	}
}

// rollbackOne does the actual stop/remove/recreate for a single container,
// shared by HandleRollback and HandleRollbackAll.
func rollbackOne(cont string, latest *SnapshotRecord) error {
	PrintInfo(fmt.Sprintf("Rolling back '%s' to snapshot from %s", cont, latest.CreatedAt.Format(time.RFC3339)))

	homeDir := os.Getenv("HOME")
	pkgType := "gui"
	initSystem := ""
	installed, _ := LoadInstalled()
	for _, ip := range installed {
		if ip.Cont == cont {
			pkgType = ip.Type
			if d, ok := Distros[ip.Distro]; ok {
				initSystem = d.InitSystem
			}
			if ip.Isolated {
				homeDir = filepath.Join(os.Getenv("HOME"), homesDir, ip.Pkg)
			}
			break
		}
	}

	ExecCommand(podmanBin, []string{"stop", cont})
	ExecCommand(podmanBin, []string{"rm", "--force", cont})

	args := getPodmanRunArgs(cont, latest.Image, homeDir, pkgType, initSystem)
	if !ExecCommand(podmanBin, args) {
		return fmt.Errorf("rollback of '%s' failed to recreate the container", cont)
	}
	PrintSuccess("Rollback complete: " + cont + " restored from " + latest.Image)
	return nil
}

// HandleRollbackAll is the real system-wide rollback: every managed
// container that has at least one recorded snapshot gets restored to its
// own latest snapshot. Containers with no snapshot are reported and
// skipped rather than silently ignored, so a partial rollback is never
// mistaken for a complete one.
func HandleRollbackAll(dryRun bool) {
	conts := GetOurContainers()
	if len(conts) == 0 {
		PrintInfo("No managed containers found")
		return
	}
	recs := loadSnapshots()

	type plan struct {
		cont   string
		latest *SnapshotRecord
	}
	var plans []plan
	var noSnapshot []string
	for _, c := range conts {
		latest := latestSnapshotFor(c, recs)
		if latest == nil {
			noSnapshot = append(noSnapshot, c)
			continue
		}
		plans = append(plans, plan{c, latest})
	}

	if len(plans) == 0 {
		PrintWarn("No managed container has a snapshot to roll back to — run 'isolator snapshot-all' first, next time")
		return
	}

	if dryRun {
		PrintInfo(fmt.Sprintf("[dry-run] Would roll back %d/%d managed container(s):", len(plans), len(conts)))
		for _, p := range plans {
			fmt.Printf("  %s  ← %s (%s)\n", p.cont, p.latest.Image, p.latest.CreatedAt.Format(time.RFC3339))
		}
		if len(noSnapshot) > 0 {
			PrintWarn("No snapshot for (would be left untouched): " + fmt.Sprint(noSnapshot))
		}
		PrintInfo("[dry-run] No changes made")
		return
	}

	PrintInfo(fmt.Sprintf("Rolling back %d/%d managed container(s) to their latest snapshot...", len(plans), len(conts)))
	failed := 0
	for _, p := range plans {
		if err := rollbackOne(p.cont, p.latest); err != nil {
			PrintError(err.Error())
			failed++
		}
	}
	if len(noSnapshot) > 0 {
		PrintWarn("Left untouched (no snapshot exists): " + fmt.Sprint(noSnapshot))
	}
	if failed == 0 {
		PrintSuccess(fmt.Sprintf("System-wide rollback complete: %d container(s) restored", len(plans)))
	} else {
		PrintWarn(fmt.Sprintf("%d/%d container(s) failed to roll back", failed, len(plans)))
	}
}

// HandleSnapshotList shows saved snapshots, most recent first.
func HandleSnapshotList() {
	recs := loadSnapshots()
	if len(recs) == 0 {
		PrintInfo("No snapshots saved yet")
		return
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].CreatedAt.After(recs[j].CreatedAt) })
	for _, r := range recs {
		fmt.Printf("  %s  %s  %s\n", CyanStyle.Render(r.Container), r.Image, DimStyle.Render(r.CreatedAt.Format(time.RFC3339)))
	}
}
