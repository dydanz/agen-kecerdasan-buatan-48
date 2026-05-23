# Graph Report - .  (2026-05-23)

## Corpus Check
- 117 files · ~102,000 words
- Verdict: corpus is large enough that graph structure adds value.

## Summary
- 397 nodes · 654 edges · 43 communities (35 shown, 8 thin omitted)
- Extraction: 73% EXTRACTED · 27% INFERRED · 0% AMBIGUOUS · INFERRED: 174 edges (avg confidence: 0.8)
- Token cost: 0 input · 0 output

## Community Hubs (Navigation)
- [[_COMMUNITY_Session Core|Session Core]]
- [[_COMMUNITY_Plugin Skills Lock|Plugin Skills Lock]]
- [[_COMMUNITY_Channel Adapters|Channel Adapters]]
- [[_COMMUNITY_GBrain Bridge|GBrain Bridge]]
- [[_COMMUNITY_LLM & Tool Registry|LLM & Tool Registry]]
- [[_COMMUNITY_Test Helpers|Test Helpers]]
- [[_COMMUNITY_Skill Resolver|Skill Resolver]]
- [[_COMMUNITY_Runtime Core|Runtime Core]]
- [[_COMMUNITY_Cold Session Opener|Cold Session Opener]]
- [[_COMMUNITY_Runtime Tests|Runtime Tests]]
- [[_COMMUNITY_Configuration|Configuration]]
- [[_COMMUNITY_MCP Client|MCP Client]]
- [[_COMMUNITY_Design Docs & PRDs|Design Docs & PRDs]]
- [[_COMMUNITY_Session Manager Tests|Session Manager Tests]]
- [[_COMMUNITY_Research & Strategy|Research & Strategy]]
- [[_COMMUNITY_Claude Settings Hooks|Claude Settings Hooks]]
- [[_COMMUNITY_Shared Types|Shared Types]]
- [[_COMMUNITY_Config Tests|Config Tests]]
- [[_COMMUNITY_Legacy Stubs|Legacy Stubs]]
- [[_COMMUNITY_Local Permissions|Local Permissions]]
- [[_COMMUNITY_Agent Identities|Agent Identities]]
- [[_COMMUNITY_Claude Agent Config|Claude Agent Config]]
- [[_COMMUNITY_Architecture Agents|Architecture Agents]]
- [[_COMMUNITY_LLM Agents|LLM Agents]]
- [[_COMMUNITY_Identity PRDs|Identity PRDs]]
- [[_COMMUNITY_Deployment Agents|Deployment Agents]]
- [[_COMMUNITY_Misc 27|Misc 27]]
- [[_COMMUNITY_Misc 38|Misc 38]]
- [[_COMMUNITY_Misc 39|Misc 39]]
- [[_COMMUNITY_Misc 40|Misc 40]]
- [[_COMMUNITY_Misc 41|Misc 41]]

## God Nodes (most connected - your core abstractions)
1. `NewSession()` - 19 edges
2. `AKB48 Runtime` - 17 edges
3. `NewMCPClient()` - 15 edges
4. `NewRegistry()` - 14 edges
5. `New()` - 14 edges
6. `writeFile()` - 12 edges
7. `contains()` - 12 edges
8. `MCPClient` - 12 edges
9. `testRuntime()` - 11 edges
10. `NewResolver()` - 11 edges

## Surprising Connections (you probably didn't know these)
- `AKB48 Runtime` --references--> `GBrain Knowledge System`  [EXTRACTED]
  internal/runtime/runtime.go → akb48-rsh/05-akb48-reference-library.md
- `GBrainBridge` --references--> `Tool Registry`  [EXTRACTED]
  phase-3-gbrain-integration.md → internal/tools/tools.go
- `newRuntimeWithOpts()` --calls--> `New()`  [INFERRED]
  tests/integration/hello_world_test.go → internal/runtime/runtime.go
- `buildTestRuntime()` --calls--> `New()`  [INFERRED]
  tests/integration/helpers_test.go → internal/runtime/runtime.go
- `TestCLIAdapter_SingleMessage()` --calls--> `contains()`  [INFERRED]
  adapters/cli/cli_test.go → internal/brain/bridge_test.go

## Communities (43 total, 8 thin omitted)

### Community 0 - "Session Core"
Cohesion: 0.12
Nodes (26): RunHooks(), makeTestTurn(), TestLogMetricsHook(), TestPersistSessionHook(), TestRunHooks_AllHooksRun(), TestRunHooks_FailingHookDoesNotBlock(), TestRunHooks_NoHooks(), TestRunHooks_PanicRecovered() (+18 more)

### Community 1 - "Plugin Skills Lock"
Cohesion: 0.08
Nodes (28): Caller, CallerInterface, CallParams, CallResult, IdempotencyKey(), NewCaller(), TestCall_ReturnsText(), TestCall_ToolLoop() (+20 more)

### Community 2 - "Channel Adapters"
Cohesion: 0.10
Nodes (19): main(), Adapter, splitMessage(), isFloodControl(), isFloodControlErr(), TestContext_CancelledAdapterStop(), TestIsAllowed_Allowed(), TestIsAllowed_EmptyList() (+11 more)

### Community 3 - "GBrain Bridge"
Cohesion: 0.07
Nodes (15): AKB48 Agent Capabilities, Memory & Learning Strategy, AKB48 Platform Landscape & Architecture Decision, AKB48 Runtime, Adapter, Context Assembler, GBrain Knowledge System, Operational Rules (AGENTS.md), markCached() (+7 more)

### Community 4 - "LLM & Tool Registry"
Cohesion: 0.17
Nodes (26): NewGBrainBridge(), containsStr(), testBrainCfg(), TestGBrainBridge_AvailableAfterStart(), TestGBrainBridge_HandlerWhenUnavailable(), TestGBrainBridge_SearchEntities_Unavailable(), TestGBrainBridge_ToolRegistration(), NewMCPClient() (+18 more)

### Community 5 - "Test Helpers"
Cohesion: 0.16
Nodes (23): contains(), NewWithIO(), captureHandler(), TestCLIAdapter_ContextCancel(), TestCLIAdapter_EchoOutput(), TestCLIAdapter_EmptyLineSkipped(), TestCLIAdapter_MultipleMessages(), TestCLIAdapter_PromptShown() (+15 more)

### Community 6 - "Skill Resolver"
Cohesion: 0.19
Nodes (17): newRuntimeWithOpts(), TestHelloWorld_BrainDegraded(), TestHelloWorld_ColdOpener(), TestHelloWorld_MemoryRoundtrip(), TestHelloWorld_MultiTurnPersistence(), TestHelloWorld_PostTurnHooks(), TestHelloWorld_SkillRouting(), buildTestRuntime() (+9 more)

### Community 7 - "Runtime Core"
Cohesion: 0.21
Nodes (10): NewSessionManager(), makeTurnN(), testCfg(), TestSessionManager_GetContextTurns_TurnLimit(), TestSessionManager_IsCold(), TestSessionManager_LoadAll(), TestSessionManager_ResolveOrCreate_LoadsFromDisk(), TestSessionManager_ResolveOrCreate_New() (+2 more)

### Community 8 - "Cold Session Opener"
Cohesion: 0.20
Nodes (16): Resolver, NewResolver(), parseSkill(), TestParseSkill_BodyExtracted(), TestParseSkill_MissingDelimiters(), TestResolve_CaseInsensitive(), TestResolve_CorruptYAML_OtherSkillsLoad(), TestResolve_EmptyDir() (+8 more)

### Community 9 - "Runtime Tests"
Cohesion: 0.18
Nodes (12): BrainSearcher, extractQuery(), NewColdOpener(), splitWords(), TestBuildColdContext_BrainError(), TestBuildColdContext_EmptyBrainResult(), TestBuildColdContext_ResumedAfterThreshold(), TestExtractQuery_EmptyMessage() (+4 more)

### Community 10 - "Configuration"
Cohesion: 0.23
Nodes (12): testRuntime(), TestRuntime_AddTurnAndRetrieve(), TestRuntime_BrainDisabled(), TestRuntime_ColdOpenerNilWhenNoBrain(), TestRuntime_FilepathFromConfig(), TestRuntime_HooksCalledOnAddTurn(), TestRuntime_MultiTurnContext(), TestRuntime_SessionManagerExposed() (+4 more)

### Community 11 - "MCP Client"
Cohesion: 0.41
Nodes (13): computedHash, skillPath, source, sourceType, skills, cavecrew, caveman, caveman-commit (+5 more)

### Community 12 - "Design Docs & PRDs"
Cohesion: 0.16
Nodes (10): AdaptersConfig, BrainConfig, CLIConfig, Config, Load(), IdentityConfig, LLMConfig, SessionConfig (+2 more)

### Community 13 - "Session Manager Tests"
Cohesion: 0.21
Nodes (5): MCPClient, rpcError, rpcRequest, rpcResponse, ToolSpec

### Community 14 - "Research & Strategy"
Cohesion: 0.33
Nodes (3): formatSearchResults(), TestFormatSearchResults_Array(), GBrainBridge

### Community 15 - "Claude Settings Hooks"
Cohesion: 0.25
Nodes (7): hooks, PreToolUse, SessionStart, permissions, allow, deny, $schema

### Community 16 - "Shared Types"
Cohesion: 0.25
Nodes (7): LLMMessage, Message, Response, Role, TokenUsage, ToolCall, ToolResult

### Community 17 - "Config Tests"
Cohesion: 0.25
Nodes (4): cachedResult, Registry, ToolDefinition, ToolHandler

### Community 19 - "Legacy Stubs"
Cohesion: 0.50
Nodes (3): permissions, allow, deny

### Community 20 - "Local Permissions"
Cohesion: 0.67
Nodes (3): AKB48 AI Agent Runtime, GBrain Knowledge Brain, Session Resolver

### Community 21 - "Agent Identities"
Cohesion: 0.67
Nodes (3): Code Reviewer Agent, Orchestrator Agent, Model Routing Rules

### Community 22 - "Claude Agent Config"
Cohesion: 0.67
Nodes (3): Go Architect Agent, Product Manager Agent, Technology Stack

### Community 23 - "Architecture Agents"
Cohesion: 0.67
Nodes (3): ContextAssembler, SkillResolver, SessionManager

## Knowledge Gaps
- **74 isolated node(s):** `version`, `ToolHandler`, `ToolDefinition`, `cachedResult`, `Role` (+69 more)
  These have ≤1 connection - possible missing edges or undocumented components.
- **8 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `New()` connect `Plugin Skills Lock` to `Cold Session Opener`, `Runtime Tests`, `LLM & Tool Registry`, `Runtime Core`?**
  _High betweenness centrality (0.001) - this node is a cross-community bridge._
- **Why does `AKB48 Runtime` connect `GBrain Bridge` to `Plugin Skills Lock`?**
  _High betweenness centrality (0.001) - this node is a cross-community bridge._
- **Are the 8 inferred relationships involving `New()` (e.g. with `NewRegistry()` and `NewCaller()`) actually correct?**
  _`New()` has 8 INFERRED edges - model-reasoned connections that need verification._
- **What connects `version`, `ToolHandler`, `ToolDefinition` to the rest of the system?**
  _76 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `Session Core` be split into smaller, more focused modules?**
  _Cohesion score 0.11942959001782531 - nodes in this community are weakly interconnected._
- **Should `Plugin Skills Lock` be split into smaller, more focused modules?**
  _Cohesion score 0.0784313725490196 - nodes in this community are weakly interconnected._
- **Should `Channel Adapters` be split into smaller, more focused modules?**
  _Cohesion score 0.10160427807486631 - nodes in this community are weakly interconnected._