# Graph Report - .  (2026-05-23)

## Corpus Check
- 117 files · ~102,022 words
- Verdict: corpus is large enough that graph structure adds value.

## Summary
- 408 nodes · 623 edges · 44 communities (36 shown, 8 thin omitted)
- Extraction: 75% EXTRACTED · 25% INFERRED · 0% AMBIGUOUS · INFERRED: 157 edges (avg confidence: 0.8)
- Token cost: 123,174 input · 3,682 output

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
- [[_COMMUNITY_Misc 28|Misc 28]]
- [[_COMMUNITY_Misc 39|Misc 39]]
- [[_COMMUNITY_Misc 40|Misc 40]]
- [[_COMMUNITY_Misc 41|Misc 41]]
- [[_COMMUNITY_Misc 42|Misc 42]]

## God Nodes (most connected - your core abstractions)
1. `NewSession()` - 21 edges
2. `NewMCPClient()` - 15 edges
3. `NewRegistry()` - 14 edges
4. `writeFile()` - 12 edges
5. `contains()` - 12 edges
6. `NewSessionManager()` - 12 edges
7. `New()` - 11 edges
8. `testRuntime()` - 11 edges
9. `NewResolver()` - 11 edges
10. `skipIfNoPython()` - 10 edges

## Surprising Connections (you probably didn't know these)
- `AKB48 Runtime` --references--> `GBrain Knowledge System`  [EXTRACTED]
  internal/runtime/runtime.go → akb48-rsh/05-akb48-reference-library.md
- `TestCLIAdapter_SingleMessage()` --calls--> `contains()`  [INFERRED]
  adapters/cli/cli_test.go → internal/brain/bridge_test.go
- `TestCLIAdapter_EchoOutput()` --calls--> `contains()`  [INFERRED]
  adapters/cli/cli_test.go → internal/brain/bridge_test.go
- `TestCLIAdapter_StreamingTokens()` --calls--> `contains()`  [INFERRED]
  adapters/cli/cli_test.go → internal/brain/bridge_test.go
- `TestCLIAdapter_PromptShown()` --calls--> `contains()`  [INFERRED]
  adapters/cli/cli_test.go → internal/brain/bridge_test.go

## Communities (44 total, 8 thin omitted)

### Community 0 - "Session Core"
Cohesion: 0.09
Nodes (27): RunHooks(), makeTestTurn(), TestLogMetricsHook(), TestPersistSessionHook(), TestRunHooks_AllHooksRun(), TestRunHooks_FailingHookDoesNotBlock(), TestRunHooks_NoHooks(), TestRunHooks_PanicRecovered() (+19 more)

### Community 1 - "Plugin Skills Lock"
Cohesion: 0.05
Nodes (37): computedHash, skillPath, source, sourceType, computedHash, skillPath, source, sourceType (+29 more)

### Community 2 - "Channel Adapters"
Cohesion: 0.10
Nodes (19): main(), Adapter, splitMessage(), isFloodControl(), isFloodControlErr(), TestContext_CancelledAdapterStop(), TestIsAllowed_Allowed(), TestIsAllowed_EmptyList() (+11 more)

### Community 3 - "GBrain Bridge"
Cohesion: 0.08
Nodes (27): AKB48 Agent Capabilities, Memory & Learning Strategy, AKB48 Platform Landscape & Architecture Decision, AKB48 Runtime, Adapter, Context Assembler, GBrain Knowledge System, Operational Rules (AGENTS.md), JSONL Persistence (+19 more)

### Community 4 - "LLM & Tool Registry"
Cohesion: 0.17
Nodes (25): NewGBrainBridge(), testBrainCfg(), TestGBrainBridge_AvailableAfterStart(), TestGBrainBridge_HandlerWhenUnavailable(), TestGBrainBridge_SearchEntities_Unavailable(), TestGBrainBridge_ToolRegistration(), NewMCPClient(), echoServerCommand() (+17 more)

### Community 5 - "Test Helpers"
Cohesion: 0.15
Nodes (24): contains(), containsStr(), NewWithIO(), captureHandler(), TestCLIAdapter_ContextCancel(), TestCLIAdapter_EchoOutput(), TestCLIAdapter_EmptyLineSkipped(), TestCLIAdapter_MultipleMessages() (+16 more)

### Community 6 - "Skill Resolver"
Cohesion: 0.20
Nodes (16): Resolver, NewResolver(), parseSkill(), TestParseSkill_BodyExtracted(), TestParseSkill_MissingDelimiters(), TestResolve_CaseInsensitive(), TestResolve_CorruptYAML_OtherSkillsLoad(), TestResolve_EmptyDir() (+8 more)

### Community 7 - "Runtime Core"
Cohesion: 0.13
Nodes (9): AKB48Runtime, ContextAssembler, MessageHandler, countSkillDirs(), New(), HookFunc, LogMetricsHook(), PersistSessionHook() (+1 more)

### Community 8 - "Cold Session Opener"
Cohesion: 0.17
Nodes (14): BrainSearcher, extractQuery(), NewColdOpener(), splitWords(), TestBuildColdContext_BrainError(), TestBuildColdContext_ColdSession(), TestBuildColdContext_EmptyBrainResult(), TestBuildColdContext_ResumedAfterThreshold() (+6 more)

### Community 9 - "Runtime Tests"
Cohesion: 0.23
Nodes (12): testRuntime(), TestRuntime_AddTurnAndRetrieve(), TestRuntime_BrainDisabled(), TestRuntime_ColdOpenerNilWhenNoBrain(), TestRuntime_FilepathFromConfig(), TestRuntime_HooksCalledOnAddTurn(), TestRuntime_MultiTurnContext(), TestRuntime_SessionManagerExposed() (+4 more)

### Community 10 - "Configuration"
Cohesion: 0.16
Nodes (10): AdaptersConfig, BrainConfig, CLIConfig, Config, Load(), IdentityConfig, LLMConfig, SessionConfig (+2 more)

### Community 11 - "MCP Client"
Cohesion: 0.19
Nodes (5): MCPClient, rpcError, rpcRequest, rpcResponse, ToolSpec

### Community 12 - "Design Docs & PRDs"
Cohesion: 0.29
Nodes (4): formatSearchResults(), TestFormatSearchResults_Array(), TestFormatSearchResults_Empty(), GBrainBridge

### Community 13 - "Session Manager Tests"
Cohesion: 0.48
Nodes (11): NewSessionManager(), makeTurnN(), testCfg(), TestSessionManager_GetContextTurns_TokenBudget(), TestSessionManager_GetContextTurns_TurnLimit(), TestSessionManager_IsCold(), TestSessionManager_LoadAll(), TestSessionManager_ResolveOrCreate_Concurrent() (+3 more)

### Community 14 - "Research & Strategy"
Cohesion: 0.25
Nodes (7): hooks, PreToolUse, SessionStart, permissions, allow, deny, $schema

### Community 15 - "Claude Settings Hooks"
Cohesion: 0.25
Nodes (7): LLMMessage, Message, Response, Role, TokenUsage, ToolCall, ToolResult

### Community 16 - "Shared Types"
Cohesion: 0.25
Nodes (4): cachedResult, Registry, ToolDefinition, ToolHandler

### Community 17 - "Config Tests"
Cohesion: 0.38
Nodes (3): markCached(), turnsToMessages(), ContextAssembler

### Community 19 - "Legacy Stubs"
Cohesion: 0.33
Nodes (6): research skill, AKB48Runtime, gbrain_search, GBrainBridge, MCPClient, ToolRegistry

### Community 20 - "Local Permissions"
Cohesion: 0.50
Nodes (3): permissions, allow, deny

### Community 21 - "Agent Identities"
Cohesion: 0.67
Nodes (3): AKB48 AI Agent Runtime, GBrain Knowledge Brain, Session Resolver

### Community 22 - "Claude Agent Config"
Cohesion: 0.67
Nodes (3): Code Reviewer Agent, Orchestrator Agent, Model Routing Rules

### Community 23 - "Architecture Agents"
Cohesion: 0.67
Nodes (3): Go Architect Agent, Product Manager Agent, Technology Stack

### Community 24 - "LLM Agents"
Cohesion: 0.67
Nodes (3): ContextAssembler, SkillResolver, SessionManager

## Knowledge Gaps
- **101 isolated node(s):** `version`, `source`, `sourceType`, `skillPath`, `computedHash` (+96 more)
  These have ≤1 connection - possible missing edges or undocumented components.
- **8 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `Adapter` connect `Channel Adapters` to `GBrain Bridge`?**
  _High betweenness centrality (0.000) - this node is a cross-community bridge._
- **What connects `version`, `source`, `sourceType` to the rest of the system?**
  _103 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `Session Core` be split into smaller, more focused modules?**
  _Cohesion score 0.08970099667774087 - nodes in this community are weakly interconnected._
- **Should `Plugin Skills Lock` be split into smaller, more focused modules?**
  _Cohesion score 0.05263157894736842 - nodes in this community are weakly interconnected._
- **Should `Channel Adapters` be split into smaller, more focused modules?**
  _Cohesion score 0.10160427807486631 - nodes in this community are weakly interconnected._
- **Should `GBrain Bridge` be split into smaller, more focused modules?**
  _Cohesion score 0.07765151515151515 - nodes in this community are weakly interconnected._
- **Should `Runtime Core` be split into smaller, more focused modules?**
  _Cohesion score 0.13071895424836602 - nodes in this community are weakly interconnected._