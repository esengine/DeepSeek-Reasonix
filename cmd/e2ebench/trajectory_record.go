package main

// trajectoryRecord is the subset of trajectory.Record the summary needs.
type trajectoryRecord struct {
	TS               int64  `json:"ts"`
	ProtocolRecovery string `json:"protocol_recovery"`
	ContractShadow   *struct {
		Intent   string `json:"intent"`
		Verdict  string `json:"verdict"`
		Complete bool   `json:"complete"`
	} `json:"contract_shadow"`
	CompletionReport *struct {
		Verdict        string   `json:"verdict"`
		Gaps           int      `json:"gaps"`
		GapKinds       []string `json:"gap_kinds"`
		ClaimsVerified int      `json:"claims_verified"`
		ClaimsUnbacked int      `json:"claims_unbacked"`
	} `json:"completion_report"`
	OutcomeProgress *struct {
		Round            int  `json:"round"`
		Exploration      int  `json:"exploration"`
		Verification     int  `json:"verification"`
		Objective        int  `json:"objective"`
		Regression       int  `json:"regression"`
		Churn            int  `json:"churn"`
		LegacyGain       int  `json:"legacy_gain"`
		Discriminating   int  `json:"discriminating"`
		DebtAge          int  `json:"debt_age"`
		BlindMutations   int  `json:"blind_mutations"`
		EBMEligible      bool `json:"ebm_eligible"`
		EBMFired         bool `json:"ebm_fired"`
		LocalExecSeen    bool `json:"local_exec_seen"`
		GovernorEligible bool `json:"governor_eligible"`
		GovernorEngaged  bool `json:"governor_engaged"`
		Runway           *int `json:"runway"`
		RunwayDry        int  `json:"runway_dry"`
		RunwayIdle       int  `json:"runway_idle"`
		RunwaySpent      bool `json:"runway_spent"`
	} `json:"outcome_progress"`
	AnchorSafetyAudit   *anchorSafetyRecord `json:"anchor_safety_audit"`
	DelegationAdmission *struct {
		Tool    string `json:"tool"`
		Verdict string `json:"verdict"`
		Reason  string `json:"reason"`
	} `json:"delegation_admission"`
	Event *struct {
		Kind          string `json:"kind"`
		Code          string `json:"code"`
		RetryScope    string `json:"retryScope"`
		StreamAttempt *struct {
			ID     string `json:"id"`
			Action string `json:"action"`
		} `json:"streamAttempt"`
		Usage *struct {
			Source           string `json:"source"`
			PromptTokens     int64  `json:"promptTokens"`
			CompletionTokens int64  `json:"completionTokens"`
			ReasoningTokens  int64  `json:"reasoningTokens"`
			CacheHitTokens   int64  `json:"cacheHitTokens"`
			CacheMissTokens  int64  `json:"cacheMissTokens"`
			CacheDiagnostics *struct {
				ToolSchemaTokens int64 `json:"toolSchemaTokens"`
				PrefixChanged    bool  `json:"prefixChanged"`
			} `json:"cacheDiagnostics"`
		} `json:"usage"`
		Tool *struct {
			ID         string `json:"id"`
			Name       string `json:"name"`
			Args       string `json:"args"`
			Err        string `json:"err"`
			DurationMs int64  `json:"durationMs"`
			ParentID   string `json:"parentId"`
			ReadOnly   bool   `json:"readOnly"`
			Refreshed  bool   `json:"refreshed"`
			StartedAt  int64  `json:"startedAt"`
			EndedAt    int64  `json:"endedAt"`
			Execution  *struct {
				Verification string `json:"verification"`
			} `json:"execution"`
		} `json:"tool"`
	} `json:"event"`
}
