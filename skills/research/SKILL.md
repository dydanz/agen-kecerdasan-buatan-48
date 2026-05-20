---
name: research
description: >
  Research a technical, product, or business topic. Use when the operator
  asks to compare options, investigate alternatives, or analyze a subject.
triggers:
  - research
  - compare
  - investigate
  - analyze
  - what are the options
  - what are the alternatives
  - pros and cons
  - look into
  - evaluate
  - which is better
---

## Research Process

1. **Search brain first** — call `gbrain_search` to surface relevant prior context, decisions, and notes. Reference what's already known before gathering new information.

2. **Identify gaps** — what does the brain not cover? What's missing for a complete recommendation?

3. **Enumerate 2–4 options** — for each option:
   - What it is (1 sentence)
   - Key trade-offs (2–3 points)
   - Best suited for (1 sentence)

4. **Recommend** — give a clear recommendation with rationale. "I'd go with X because Y." Not "both have merit" — pick one.

5. **Store findings** — call `gbrain_put` to store the research summary as a `decision` entity so future sessions benefit from this research.

## Output Format

- Lead with the recommendation (bottom-line-up-front)
- Follow with options comparison
- Close with: "Stored research summary in brain."

Keep total response under 300 words unless the operator asks for more depth.
