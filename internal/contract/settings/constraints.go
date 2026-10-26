package settings

import "reasonix/internal/contract/config"

// constraints say what a legal value of a writable key is: the config's own
// setter where it has one, else a bound, else an explicit statement that any
// value of the key's shape is legal. The coverage test fails a writable string
// or number with none of the three.
var constraints = map[string]Entry{
	"observability.otlp.endpoint":                   {Via: viaString((*config.Config).SetOTLPEndpoint)},
	"observability.otlp.header_name":                {Via: viaString((*config.Config).SetOTLPHeaderName)},
	"observability.otlp.header_env":                 {Via: viaString((*config.Config).SetOTLPHeaderEnv)},
	"observability.otlp.service_name":               {Free: true},
	"tools.web_search_backends":                     {Via: backendList(config.KnownSearchBackends, func(c *config.Config) *[]string { return &c.Tools.WebSearchBackends })},
	"tools.web_fetch_backends":                      {Via: backendList(config.KnownFetchBackends, func(c *config.Config) *[]string { return &c.Tools.WebFetchBackends })},
	"ui.shortcut_layout":                            {Via: viaString((*config.Config).SetUIShortcutLayout)},
	"language":                                      {Via: viaString((*config.Config).SetLanguage)},
	"agent.reasoning_language":                      {Via: viaString((*config.Config).SetReasoningLanguage)},
	"desktop.language":                              {Via: viaString((*config.Config).SetDesktopLanguage)},
	"desktop.terminal_theme":                        {Via: viaString((*config.Config).SetDesktopTerminalTheme)},
	"desktop.close_behavior":                        {Via: viaString((*config.Config).SetDesktopCloseBehavior)},
	"desktop.display_mode":                          {Via: viaString((*config.Config).SetDesktopDisplayMode)},
	"desktop.status_bar_style":                      {Via: viaString((*config.Config).SetDesktopStatusBarStyle)},
	"ui.close_behavior":                             {Via: viaString((*config.Config).SetUICloseBehavior)},
	"desktop.conversation_width":                    {Via: viaString((*config.Config).SetDesktopConversationWidth)},
	"desktop.tray":                                  {Via: viaString((*config.Config).SetDesktopTray)},
	"billing.display_currency":                      {Via: viaString((*config.Config).SetDisplayCurrency)},
	"desktop.currency":                              {Via: viaString((*config.Config).SetDesktopCurrency)},
	"desktop.reasoning_display_mode":                {Via: viaString((*config.Config).SetDesktopReasoningDisplayMode)},
	"agent.auto_plan":                               {Via: viaString((*config.Config).SetAutoPlan)},
	"agent.default_mode":                            {Via: viaString((*config.Config).SetDefaultMode)},
	"agent.web_search_model":                        {Via: viaString((*config.Config).SetWebSearchModel)},
	"agent.cold_resume_prune":                       {Via: viaBool((*config.Config).SetColdResumePrune)},
	"ui.show_reasoning":                             {Via: viaBool((*config.Config).SetShowReasoning)},
	"desktop.expand_thinking":                       {Via: viaBool((*config.Config).SetExpandThinking)},
	"desktop.status_bar_items":                      {Via: viaList((*config.Config).SetDesktopStatusBarItems)},
	"agent.compact_ratio":                           {Via: viaFloat((*config.Config).SetCompactRatio)},
	"agent.context_soft_limit_tokens":               {Via: viaInt((*config.Config).SetContextSoftLimitTokens)},
	"agent.advisor_model":                           {Free: true},
	"agent.auto_plan_classifier":                    {Free: true},
	"agent.context_editing":                         {Free: true},
	"agent.decision_model":                          {Free: true},
	"agent.output_style":                            {Free: true},
	"agent.planner_model":                           {Free: true},
	"agent.recovery_model":                          {Free: true},
	"agent.subagent_effort":                         {Free: true},
	"agent.subagent_model":                          {Free: true},
	"agent.title_model":                             {Free: true},
	"agent.triage_model":                            {Free: true},
	"agent.turn_narration":                          {Free: true},
	"agent.vision_model":                            {Free: true},
	"desktop.appearance.font_mono":                  {Free: true},
	"desktop.appearance.font_ui":                    {Free: true},
	"desktop.appearance.wallpaper.file":             {Free: true},
	"desktop.appearance.wallpaper.kind":             {Free: true},
	"desktop.appearance.wallpaper.title":            {Free: true},
	"desktop.appearance.wallpaper.audio":            {Free: true},
	"desktop.appearance.wallpaper.audios":           {Free: true},
	"desktop.appearance.wallpaper.audio_volumes":    {Free: true},
	"desktop.appearance.wallpaper.audio_loops":      {Free: true},
	"desktop.appearance.wallpaper.layers":           {Free: true},
	"desktop.appearance.wallpaper.scene_width":      {Free: true},
	"desktop.appearance.wallpaper.scene_height":     {Free: true},
	"desktop.appearance.wallpaper.parallax":         {Min: 0, Max: 0.2, Bounded: true},
	"desktop.appearance.wallpaper.volume":           {Min: 0, Max: 1, Bounded: true},
	"desktop.theme":                                 {Free: true},
	"desktop.theme_pack":                            {Free: true},
	"desktop.theme_style":                           {Free: true},
	"ui.commandmode":                                {Free: true},
	"ui.cursor_shape":                               {Free: true},
	"ui.theme":                                      {Free: true},
	"ui.theme_style":                                {Free: true},
	"tools.search.engine":                           {Free: true},
	"agent.write_lease":                             {Free: true},
	"desktop.appearance.read_size":                  {Free: true},
	"desktop.appearance.text_size":                  {Free: true},
	"desktop.appearance.zoom":                       {Free: true},
	"agent.keep":                                    {Free: true},
	"skills.disabled_skills":                        {Free: true},
	"mods.hook_budget_ms":                           {Min: 0, Max: 55000, Bounded: true},
	"mods.context_bytes":                            {Min: 0, Max: 1 << 24, Bounded: true},
	"mods.context_blocks":                           {Min: 0, Max: 1024, Bounded: true},
	"mods.messages_max":                             {Min: 0, Max: 100000, Bounded: true},
	"mods.submit_per_turn":                          {Min: 0, Max: 1000, Bounded: true},
	"mods.submit_bytes":                             {Min: 0, Max: 1 << 24, Bounded: true},
	"agent.checkpoint_ceiling_ratio":                {Min: 0.05, Max: 1, Bounded: true},
	"agent.compact_force_ratio":                     {Min: 0.05, Max: 1, Bounded: true},
	"agent.soft_compact_ratio":                      {Min: 0.05, Max: 1, Bounded: true},
	"agent.tool_result_snip_ratio":                  {Min: 0.05, Max: 1, Bounded: true},
	"agent.temperature":                             {Min: 0, Max: 2, Bounded: true},
	"agent.recovery_temperature":                    {Min: 0, Max: 2, Bounded: true},
	"desktop.appearance.wallpaper.dim":              {Min: 0, Max: 1, Bounded: true},
	"desktop.appearance.wallpaper.focus_x":          {Min: 0, Max: 1, Bounded: true},
	"desktop.appearance.wallpaper.focus_y":          {Min: 0, Max: 1, Bounded: true},
	"desktop.appearance.wallpaper.opacity":          {Min: 0, Max: 1, Bounded: true},
	"agent.first_turn_pin_tokens":                   {Min: 0, Max: 1e9, Bounded: true},
	"agent.max_provider_retries":                    {Min: 0, Max: 1e9, Bounded: true},
	"agent.max_queued_inputs":                       {Min: 0, Max: 1e9, Bounded: true},
	"agent.recent_keep":                             {Min: 0, Max: 1e9, Bounded: true},
	"agent.user_turn_keep_tokens":                   {Min: 0, Max: 1e9, Bounded: true},
	"auto_archive.days":                             {Min: 0, Max: 1e9, Bounded: true},
	"desktop.max_workspaces":                        {Min: 0, Max: 1e9, Bounded: true},
	"desktop.session_ref_tokens":                    {Min: 500, Max: 200000, Bounded: true},
	"memory.pinned_budget_chars":                    {Min: 0, Max: 1e9, Bounded: true},
	"memory.recall_limit":                           {Min: 0, Max: 1e9, Bounded: true},
	"memory.recall_max_chars":                       {Min: 0, Max: 1e9, Bounded: true},
	"progress_watch.perseveration_retries":          {Min: 0, Max: 1e9, Bounded: true},
	"progress_watch.rounds":                         {Min: 0, Max: 1e9, Bounded: true},
	"progress_watch.token_multiple":                 {Min: 0, Max: 1e9, Bounded: true},
	"skills.max_depth":                              {Min: 0, Max: 1e9, Bounded: true},
	"tools.background_jobs.stalled_warning_seconds": {Min: 0, Max: 1e9, Bounded: true},
	"tools.bash_timeout_seconds":                    {Min: 0, Max: 1e9, Bounded: true},
	"tools.mcp_call_timeout_seconds":                {Min: 0, Max: 1e9, Bounded: true},
	"tools.mcp_startup_timeout_seconds":             {Min: 0, Max: 1e9, Bounded: true},
}

func viaString(set func(*config.Config, string) error) func(*config.Config, any) error {
	return func(c *config.Config, v any) error { return set(c, v.(string)) }
}

func viaBool(set func(*config.Config, bool) error) func(*config.Config, any) error {
	return func(c *config.Config, v any) error { return set(c, v.(bool)) }
}

func viaInt(set func(*config.Config, int) error) func(*config.Config, any) error {
	return func(c *config.Config, v any) error { return set(c, v.(int)) }
}

func viaFloat(set func(*config.Config, float64) error) func(*config.Config, any) error {
	return func(c *config.Config, v any) error { return set(c, v.(float64)) }
}

func viaList(set func(*config.Config, []string) error) func(*config.Config, any) error {
	return func(c *config.Config, v any) error { return set(c, v.([]string)) }
}

// backendList writes a web backend chain through the config's own normaliser, so
// a name it cannot build is refused here instead of at the next session's start.
func backendList(known func() []string, field func(*config.Config) *[]string) func(*config.Config, any) error {
	return func(c *config.Config, v any) error {
		list, err := config.NormalizeWebBackends(v.([]string), known())
		if err != nil {
			return err
		}
		*field(c) = list
		return nil
	}
}
