# ADR 0028: Chinese-First Product Language

- Status: Accepted product-language decision; implementation NOT RUN
- Date: 2026-07-26

## Context

The current frontend mixes English operational copy with isolated Chinese controls, making the personal product slower to scan and harder to demonstrate. A complete bilingual localization system would add maintenance cost that is not justified for one Owner.

## Decision

The product interface is Chinese-first. Navigation, headings, filters, commands, validation, feedback, errors, empty states, settings, help text and Agent responses use Chinese by default.

Canonical professional terms and source identities remain in their authoritative form, including Kubernetes, Prometheus, Alertmanager, Agent, GitHub, Argo CD, Pod, Deployment, Trace, resource Kind, status and reason codes, log content, query languages, hashes and configuration keys. Chinese explanation can accompany a professional term where it improves comprehension, but the original value is never replaced or mistranslated.

The first redesign release does not provide a language switch or a complete bilingual localization surface.

## Consequences

- Existing English-only frontend copy and mixed-language controls must be rewritten consistently.
- Provider and API error codes remain visible beside a Chinese explanation so diagnosis does not lose source fidelity.
- Agent prompts and response presentation request Chinese output while preserving quoted source facts and professional identifiers.
- Layout and component tests must include realistic long Chinese labels and mixed Chinese-English technical strings.

## Rejected Alternative

Maintain an English-first interface or build a full Chinese-English switch for a single-user personal project.
