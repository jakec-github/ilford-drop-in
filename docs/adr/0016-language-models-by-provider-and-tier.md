# Language models by provider and tier

Status: proposed, 2026-11-02. Partly supersedes
[ADR 0015](0015-plain-english-patterns-one-translation-per-generate.md): its
"No provider abstraction" and its model named in server config.

## In plain words

The natural editor, and the eval's judge, ask a language model. Until now that
was always Anthropic's, called directly, and the model was named by its id —
`claude-sonnet-5-5` in config and code. Mistral is about to be added as a
second provider, so the code now asks for a **tier** — `small`, `medium` or
`large` — and each deployment's config says which provider answers.

## Decisions and their reasons

- **Providers sit behind `llm.Model`.** The interface ADR 0015 kept for tests
  and the dev stack is now also the seam between providers. Each provider has
  its own package under `pkg/core/llm`, and nothing outside it imports its SDK.

- **Code names a tier, never a model.** The translator asks for `medium`. Each
  provider maps its tiers to its own models, with their list prices, in its
  package. Moving a tier to a newer model is one line there, not a config change
  in every deployment.

- **Config names the provider, once per deployment.** `llm.provider` and
  `llm.apiKey` serve the whole server, as `translator.apiKey` did. Every provider
  so far needs only a key, so there is one key field, not a block per provider;
  a provider that needs more, such as an endpoint, is when that changes.

- **Effort is general.** It stays one setting, passed to every provider; one
  with nothing like it ignores it.

- **The eval names `<provider>-<tier>`.** `-model mistral-medium -judge
  anthropic-large` mixes providers in one run, each with its own key from the
  environment. A report records the tier asked for and the model it resolved to,
  so an old report still says what ran after a tier moves.

## Considered and rejected

- **Model ids in config, as before.** Every deployment would need editing to
  move to a newer model, and the eval's names would not carry across providers.

- **One block per provider in config, exactly one set.** More shape than one
  key needs today.

- **A tier per use in config.** Nothing yet wants the translator on anything
  but `medium`; it is a field to add when something does.

## Consequences

- A tier's model, and so its behaviour, can change in a release with no config
  change. The eval is how that is checked before it ships.
- The `translator:` key is gone. A config still carrying it warns and has no
  natural editor until it is rewritten as `llm:`.
