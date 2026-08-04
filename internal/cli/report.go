package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/crashreport"
	"reasonix/internal/i18n"
)

var sendCLIReport = crashreport.Send

func reportCommand(args []string) int {
	return reportCommandWithIO(args, cliIsInteractive(), os.Stdin, os.Stdout, os.Stderr)
}

func reportCommandWithIO(args []string, interactive bool, in io.Reader, out, errOut io.Writer) int {
	home := config.ReasonixHomeDir()
	action := ""
	id := ""
	if len(args) > 0 {
		action = strings.ToLower(strings.TrimSpace(args[0]))
	}
	if len(args) > 1 {
		id = strings.TrimSpace(args[1])
	}
	if len(args) > 2 {
		reportUsage(errOut)
		return 2
	}

	switch action {
	case "list":
		if id != "" {
			reportUsage(errOut)
			return 2
		}
		reports, err := crashreport.List(home)
		if err != nil {
			fmt.Fprintln(errOut, i18n.M.ErrorPrefix, err)
			return 1
		}
		if len(reports) == 0 {
			fmt.Fprintln(out, i18n.M.ReportNoPending)
			return 0
		}
		for _, pending := range reports {
			fmt.Fprintf(out, "%s  %s  %s/%s  %s\n", pending.ID, pending.CapturedAt().Format(time.RFC3339), pending.Report.OS, pending.Report.Arch, pending.Report.Version)
		}
		return 0
	case "show":
		return showCLIReport(home, id, out, errOut)
	case "send":
		pending, err := loadCLIReport(home, id, errOut)
		if err != nil {
			return reportLoadResult(err, out)
		}
		return sendPendingCLIReport(home, pending, out, errOut)
	case "delete":
		pending, err := loadCLIReport(home, id, errOut)
		if err != nil {
			return reportLoadResult(err, out)
		}
		if err := crashreport.Remove(home, pending.ID); err != nil {
			fmt.Fprintln(errOut, i18n.M.ErrorPrefix, err)
			return 1
		}
		fmt.Fprintf(out, i18n.M.ReportDeletedFmt+"\n", pending.ID)
		return 0
	case "":
		pending, err := loadCLIReport(home, "", errOut)
		if err != nil {
			return reportLoadResult(err, out)
		}
		if rc := printCLIReport(pending, out, errOut); rc != 0 {
			return rc
		}
		if !interactive {
			fmt.Fprintf(out, "\n"+i18n.M.ReportPreviewOnlyFmt+"\n", pending.ID)
			return 0
		}
		answer := ask(bufio.NewScanner(in), out, "\n"+i18n.M.ReportSendPrompt, "y/N")
		if !strings.EqualFold(strings.TrimSpace(answer), "y") && !strings.EqualFold(strings.TrimSpace(answer), "yes") {
			fmt.Fprintln(out, i18n.M.ReportKept)
			return 0
		}
		return sendPendingCLIReport(home, pending, out, errOut)
	default:
		reportUsage(errOut)
		return 2
	}
}

func loadCLIReport(home, id string, errOut io.Writer) (crashreport.Pending, error) {
	pending, err := crashreport.Load(home, id)
	if err != nil {
		if !errors.Is(err, crashreport.ErrNoReports) {
			fmt.Fprintln(errOut, i18n.M.ErrorPrefix, err)
		}
		return crashreport.Pending{}, err
	}
	return pending, nil
}

func showCLIReport(home, id string, out, errOut io.Writer) int {
	pending, err := loadCLIReport(home, id, errOut)
	if err != nil {
		return reportLoadResult(err, out)
	}
	return printCLIReport(pending, out, errOut)
}

func reportLoadResult(err error, out io.Writer) int {
	if errors.Is(err, crashreport.ErrNoReports) {
		fmt.Fprintln(out, i18n.M.ReportNoPending)
		return 0
	}
	return 1
}

func printCLIReport(pending crashreport.Pending, out, errOut io.Writer) int {
	preview, err := crashreport.Preview(pending.Report)
	if err != nil {
		fmt.Fprintln(errOut, i18n.M.ErrorPrefix, err)
		return 1
	}
	fmt.Fprintf(out, i18n.M.ReportHeaderFmt+"\n"+i18n.M.ReportCapturedFmt+"\n\n%s\n", pending.ID, pending.CapturedAt().Format(time.RFC3339), preview)
	return 0
}

func sendPendingCLIReport(home string, pending crashreport.Pending, out, errOut io.Writer) int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(errOut, i18n.M.ErrorPrefix, fmt.Sprintf(i18n.M.ReportConfigFailedFmt, err))
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := sendCLIReport(ctx, pending.Report, cfg.NetworkProxySpec()); err != nil {
		fmt.Fprintln(errOut, i18n.M.ErrorPrefix, fmt.Sprintf(i18n.M.ReportUploadFailedFmt, err))
		return 1
	}
	if err := crashreport.Remove(home, pending.ID); err != nil {
		fmt.Fprintln(errOut, i18n.M.ErrorPrefix, fmt.Sprintf(i18n.M.ReportSentDeleteFailedFmt, err))
		return 1
	}
	fmt.Fprintf(out, i18n.M.ReportSentFmt+"\n", pending.ID)
	return 0
}

func reportUsage(w io.Writer) {
	fmt.Fprintln(w, i18n.M.ReportUsageBody)
}
