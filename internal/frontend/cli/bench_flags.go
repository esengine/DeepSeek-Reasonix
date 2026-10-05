// bench_flags.go — the flags a benchmark arm is selected with.
package cli

import (
	"github.com/spf13/pflag"

	"reasonix/internal/contract/ablation"
)

// registerArmFlags declares both benchmark axes together, so a frontend cannot
// offer one without the other and run an index arm under the control's name.
func registerArmFlags(fs *pflag.FlagSet) (ablate, foldIndex *string) {
	return fs.String("ablate", "", "benchmark arm: comma-separated subsystems to switch off ("+ablation.ModuleList()+"; none|all)"),
		fs.String("fold-index", "", "benchmark arm: how much of the model-visible fold index to keep (default|half|quarter|off)")
}
