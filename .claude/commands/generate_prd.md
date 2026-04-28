# /generate-prd

Generates a structured Product Requirements Document (PRD) by orchestrating research and product management agents to capture requirements, user stories, and success metrics.

## When Invoked

If no topic is provided, ask:

```
I'll help you create a Product Requirements Document. To get started:

1. **Feature or product area:** What are we defining requirements for?
2. **User context:** Who are the primary users? What problem do they have?
3. **Business context:** Why is this a priority now?
4. **Constraints:** Any known technical, timeline, or resource constraints?
5. **Existing research:** Do you have user feedback, data, or a rough spec to work from?

Let's build a PRD that engineering can implement from.
```

## PRD Generation Process

### Step 1: Research (if needed)

If the feature lacks sufficient context, invoke the `product-research` agent to:
- Research the user segment and their needs
- Analyze comparable solutions in the market
- Identify key assumptions to validate

### Step 2: Problem Framing

Before writing requirements, align on the problem statement:

```
Draft problem statement:

"[Target user] struggle with [specific problem] when [context].
This results in [impact/consequence].
Today they [current workaround], which is [why workaround is insufficient].
We believe [proposed solution] will [expected improvement]."

Does this accurately capture the problem? Should anything be adjusted?
```

Do not proceed until the problem statement is confirmed.

### Step 3: Requirements Workshop

Work through the requirements iteratively:

**Round 1 — Core user stories:**
Present 3-5 core user stories covering the main workflows.
Ask: "Are these the right stories? Are any missing?"

**Round 2 — Edge cases:**
Present 2-3 edge case stories (error states, empty states, limits).
Ask: "What else could go wrong that we should define behavior for?"

**Round 3 — Success metrics:**
Present proposed metrics with baseline and targets.
Ask: "Are these the right measures of success?"

### Step 4: Write the PRD

Use the `product-manager` agent to produce the full PRD document.
Save to `.claude/thoughts/prd/YYYY-MM-DD-[feature-name].md`

Present the document with:
```
PRD draft is ready: .claude/thoughts/prd/YYYY-MM-DD-[feature-name].md

Summary:
- X user stories (Y must-have, Z should-have)
- Primary metric: [metric] from baseline → target
- Target release: [date]
- Open questions: [count] — listed at the end of the document

Recommended next step: /design-architecture to translate this into a technical design.
```

### Step 5: Architecture Handoff (Optional)

If the user wants to proceed immediately to technical design:
Invoke the `tech-lead-architect` agent to create the Technical Design Document.

## Important Guidelines

- Problem statement must be confirmed before writing requirements
- Every requirement must be traceable to a user need — no orphan requirements
- Success metrics must have baselines — avoid "increase by 20%" with no starting point
- Out-of-scope section is mandatory — it prevents scope expansion during development
- Open questions must be explicitly listed — no hidden assumptions
- Save PRD to `.claude/thoughts/prd/` — reference it in all subsequent planning work
