package pipeline

type Stage int

const (
	StageDiscovery Stage = iota
	StageDiscoveryBrief
	StageDiscoveryGate
	StageDiscoveryDeep
	StageSecurity
	StagePRD
	StageValidatePRD
	StageDone
)

func (s Stage) String() string {
	switch s {
	case StageDiscovery:
		return "discovery"
	case StageDiscoveryBrief:
		return "discovery_brief"
	case StageDiscoveryGate:
		return "discovery_gate"
	case StageDiscoveryDeep:
		return "discovery_deep"
	case StageSecurity:
		return "security"
	case StagePRD:
		return "prd"
	case StageValidatePRD:
		return "validate_prd"
	case StageDone:
		return "done"
	default:
		return "unknown"
	}
}

func (s Stage) Next() Stage {
	switch s {
	case StageDiscovery:
		return StageDiscoveryBrief
	case StageDiscoveryBrief:
		return StageDiscoveryGate
	case StageDiscoveryGate:
		return StageSecurity
	case StageDiscoveryDeep:
		return StageSecurity
	case StageSecurity:
		return StagePRD
	case StagePRD:
		return StageValidatePRD
	case StageValidatePRD:
		return StageDone
	default:
		return StageDone
	}
}

type LLDStage int

const (
	LLDStageErd LLDStage = iota
	LLDStageApi
	LLDStagePlan
	LLDStageValidate
	LLDStageDone
)

func (s LLDStage) String() string {
	switch s {
	case LLDStageErd:
		return "erd"
	case LLDStageApi:
		return "api"
	case LLDStagePlan:
		return "plan"
	case LLDStageValidate:
		return "validate"
	case LLDStageDone:
		return "done"
	default:
		return "unknown"
	}
}

func (s LLDStage) Next() LLDStage {
	switch s {
	case LLDStageErd:
		return LLDStageApi
	case LLDStageApi:
		return LLDStagePlan
	case LLDStagePlan:
		return LLDStageValidate
	case LLDStageValidate:
		return LLDStageDone
	default:
		return LLDStageDone
	}
}
