# Ironclaw Feature Designs

Detailed designs for proposed patterns and features for [nearai/ironclaw](https://github.com/nearai/ironclaw).

## Design Documents

| # | Category | File |
|---|----------|------|
| 1 | Agent Reliability & Self-Correction | [01-agent-reliability.md](01-agent-reliability.md) |
| 2 | Context & Memory | [02-context-and-memory.md](02-context-and-memory.md) |
| 3 | Safety & Security Enhancements | [03-safety-and-security.md](03-safety-and-security.md) |
| 4 | Tool System | [04-tool-system.md](04-tool-system.md) |
| 5 | Observability & Debugging | [05-observability-and-debugging.md](05-observability-and-debugging.md) |
| 6 | Developer Experience | [06-developer-experience.md](06-developer-experience.md) |
| 7 | Workflow Patterns | [07-workflow-patterns.md](07-workflow-patterns.md) |


---

## Summary Matrix

| Feature | Complexity | Dependencies | Value |
|---|---|---|---|
| 1.1 Strategy Rotation | Medium | worker, tools | High — directly addresses user complaints |
| 1.2 Checkpoint & Rollback | High | job_manager, db | High — enables reliable long-running jobs |
| 1.3 Verification Gates | Medium | worker, tools | High — prevents false success |
| 2.1 Tiered Context | High | context, db, pgvector | High — core scalability improvement |
| 2.2 Auto Summarization | Medium | context, llm | High — enables long sessions |
| 2.3 Goal Tracking | Medium | context, db | Medium — nice UX improvement |
| 3.1 Graduated Permissions | Medium | sandbox, tools | Medium — better security model |
| 3.2 Output Diffing | Medium | safety, tools | High — user trust and control |
| 3.3 Audit Dashboard | Medium | db, web UI | Medium — operational visibility |
| 4.1 Tool Pipelines | High | tools, registry | Medium — power user feature |
| 4.2 Tool Recommendations | Medium | registry, embedding | Medium — ecosystem growth |
| 4.3 WASM Hot-Reload | Low | tools, sandbox | High — developer experience |
| 5.1 Execution Traces | High | worker, observability | High — debugging foundation |
| 5.2 Cost Tracking | Low | llm, db | High — operational necessity |
| 5.3 Replay System | Medium | traces, checkpoints | Medium — advanced debugging |
| 6.1 Dev Mode | Medium | config, tools, llm | High — developer experience |
| 6.2 Schema-Driven Codegen | High | tools, codegen | Medium — onboarding acceleration |
| 6.3 Model Fallback Chain | Medium | llm | High — resilience |
| 7.1 Event Sourcing | High | db, job_manager | High — architectural improvement |
| 7.2 Dead Letter Queue | Medium | tools, db | Medium — operational reliability |
| 7.3 Declarative Routines | Medium | routines, tools | High — accessibility |

**Recommended implementation order (highest impact, least dependencies first):**
1. Cost Tracking (5.2) — low effort, immediate value
2. WASM Hot-Reload (4.3) — low effort, unblocks tool development
3. Verification Gates (1.3) — addresses top user complaint
4. Output Diffing (3.2) — builds user trust
5. Execution Traces (5.1) — foundation for replay, debugging
6. Strategy Rotation (1.1) — addresses top user complaint
7. Auto Summarization (2.2) — enables long sessions
8. Model Fallback Chain (6.3) — resilience
9. Everything else in dependency order
