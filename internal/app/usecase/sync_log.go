package usecase

import (
	"bytes"
	"fmt"
	"text/tabwriter"

	"github.com/olegshirko/reposqueeze/internal/domain/entity"
)

// MirrorLogLines renders a mirror's origin and journal as an aligned table.
func MirrorLogLines(m entity.Mirror) []string {
	var buf bytes.Buffer
	w := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "  WHEN\tDIR\tLOCAL\tGITLAB\tPULLED\tPUSHED\tNOTES")
	fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t\t\torigin\n", m.Origin.At.Local().Format("2006-01-02 15:04"), entity.SyncInit,
		short(m.Origin.LocalSHA), short(m.Origin.RemoteSHA))
	for _, j := range m.Journal {
		notes := ""
		if len(j.Merged) > 0 {
			notes += fmt.Sprintf("merged %d ", len(j.Merged))
		}
		if len(j.Conflicts) > 0 {
			notes += fmt.Sprintf("conflicts: %v ", j.Conflicts)
		}
		if len(j.Replayed) > 0 {
			notes += fmt.Sprintf("replayed %d commit(s) ", len(j.Replayed))
		}
		if j.RemoteMoved {
			notes += "gitlab moved during sync"
		}
		fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t%d\t%d\t%s\n", j.At.Local().Format("2006-01-02 15:04"), j.Direction,
			short(j.LocalSHA), short(j.RemoteSHA), j.Pulled, j.Pushed, notes)
	}
	w.Flush()

	lines := []string{fmt.Sprintf("Mirror %s  (local %s  <->  %s/%s, project id %d)",
		m.Name, m.LocalBranch, m.ProjectName, m.RemoteBranch, m.ProjectID)}
	for _, l := range bytes.Split(bytes.TrimRight(buf.Bytes(), "\n"), []byte("\n")) {
		lines = append(lines, string(l))
	}
	for _, j := range m.Journal {
		if len(j.Replayed) == 0 {
			continue
		}
		lines = append(lines, fmt.Sprintf("  replayed %s:", j.At.Local().Format("2006-01-02 15:04")))
		for _, p := range j.Replayed {
			lines = append(lines, fmt.Sprintf("    gitlab %s -> local %s  %s", short(p.RemoteSHA), short(p.LocalSHA), p.Title))
		}
	}
	if m.PendingMerge != nil {
		lines = append(lines, fmt.Sprintf("  pending conflicts: %v", m.PendingMerge.Files))
	}
	return lines
}
