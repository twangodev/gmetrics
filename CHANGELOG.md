# Changelog

## [1.6.0](https://github.com/twangodev/gmetrics/compare/v1.5.0...v1.6.0) (2026-06-05)


### Features

* **render:** fall back to Noto Sans KR for CJK glyphs ([#18](https://github.com/twangodev/gmetrics/issues/18)) ([1b86662](https://github.com/twangodev/gmetrics/commit/1b86662c5f2d343f43196c71eb7ca2dd7a44d5ef))

## [1.5.0](https://github.com/twangodev/gmetrics/compare/v1.4.0...v1.5.0) (2026-05-30)


### Features

* **base:** track GitHub hireable status for the badge ([#15](https://github.com/twangodev/gmetrics/issues/15)) ([0c3e7b1](https://github.com/twangodev/gmetrics/commit/0c3e7b1999977fe6a2c0751fa9557e661bf003ae))

## [1.4.0](https://github.com/twangodev/gmetrics/compare/v1.3.0...v1.4.0) (2026-05-30)


### Features

* **render:** rounded thumbnail corners (music art + steam icons) ([cf5ad21](https://github.com/twangodev/gmetrics/commit/cf5ad21683f0e4989f426a62ddc728b74376e474))
* **steam:** per-game stats — playtime share, last played, platform, achievements ([cf201fc](https://github.com/twangodev/gmetrics/commit/cf201fca475dc6029f38fcbaffcff8a612e09796))
* **wakatime:** relative bars scaled by time, labels in time units ([#14](https://github.com/twangodev/gmetrics/issues/14)) ([02498fa](https://github.com/twangodev/gmetrics/commit/02498fa5ede1a52c381220791fede80e8290a6e7))
* **wakatime:** round the bar-chart bars ([#12](https://github.com/twangodev/gmetrics/issues/12)) ([75990e8](https://github.com/twangodev/gmetrics/commit/75990e84978b1ca15c699f773668b8f7c35037fd))


### Bug Fixes

* **base:** skip GraphQL and omit fragment when no sections ([#13](https://github.com/twangodev/gmetrics/issues/13)) ([9724a17](https://github.com/twangodev/gmetrics/commit/9724a17a4ea05ea02db521e5f58c3fa7477632ef))
* **wakatime:** render chart text in the card body gray ([33bc515](https://github.com/twangodev/gmetrics/commit/33bc515a55162ffb2917aed4c71415f6ba6bbf78))

## [1.3.0](https://github.com/twangodev/gmetrics/compare/v1.2.0...v1.3.0) (2026-05-29)


### Features

* **languages:** export ColorFor for cross-plugin reuse ([a296604](https://github.com/twangodev/gmetrics/commit/a296604dc802f5cd0332307f2a371202b9f2ef35))
* **music:** 2-column 8-track grid with centered artwork text ([aae8ce8](https://github.com/twangodev/gmetrics/commit/aae8ce869bae52c42172ca9488c77397eaf31d29))
* **render:** hoist pixel-aware TruncateToWidth helper ([81da5be](https://github.com/twangodev/gmetrics/commit/81da5be742c45482fa73dc3226a82f541ae94b98))
* **wakatime:** 2-column prose+graphs with accurate per-category colors ([655354c](https://github.com/twangodev/gmetrics/commit/655354c3827a9b0ffe8ded1f4577a2bf17b66afd))

## [1.2.0](https://github.com/twangodev/gmetrics/compare/v1.1.0...v1.2.0) (2026-05-27)


### Features

* **action:** cache indepth stats automatically via composite action ([364d45c](https://github.com/twangodev/gmetrics/commit/364d45cbee41128bdba9dbdb0cfa0b08b94bdbcb))
* **githubapi:** rate-limit middleware and Search throttle on transport ([ff96fc8](https://github.com/twangodev/gmetrics/commit/ff96fc87a452b47152a603fb9c815e8ad536643a))
* **languages:** authenticate indepth clones and emit categorized walk errors ([b714981](https://github.com/twangodev/gmetrics/commit/b71498125c9d9a01966dde0b4257e66851e99e8f))
* **languages:** auto-quiet indepth logs when CI=true ([fc575f6](https://github.com/twangodev/gmetrics/commit/fc575f68f36b6b42ea51d28a90569d1ed984d29b))
* **languages:** carry pushed_at and default branch on repo tasks ([5eb7caf](https://github.com/twangodev/gmetrics/commit/5eb7caf80f4a62a21f7e7906c8fbf2735852c348))
* **languages:** compare-API incremental fold for changed repos ([31e3770](https://github.com/twangodev/gmetrics/commit/31e37707454deb937f9068f8301bdfaa5c80bb74))
* **languages:** expose indepth cache path input and document actions/cache ([6de6602](https://github.com/twangodev/gmetrics/commit/6de66029535c7914e3feb2f76c126acfa67f6ef5))
* **languages:** incremental indepth cache with compare-fold gate ([d6e6ce5](https://github.com/twangodev/gmetrics/commit/d6e6ce543efdad77875fb24d794541fa06782036))
* **languages:** indepth result cache file (load/save/hash/prune) ([2256995](https://github.com/twangodev/gmetrics/commit/22569958e837c3649a7c9691f1844be09e57cc4c))
* **languages:** sanitize indepth error logs and skip inaccessible repos ([a1170d7](https://github.com/twangodev/gmetrics/commit/a1170d7b495a2eb1081be34dc7236997263424cf))


### Bug Fixes

* **languages:** capture clone tip SHA to prevent watermark drift ([759a531](https://github.com/twangodev/gmetrics/commit/759a5312398a3ab9f85467c8c7a7567b376fb2f8))
* **languages:** correct Search semantics and address PR review ([5b7e1a0](https://github.com/twangodev/gmetrics/commit/5b7e1a0b23ed18675048302ec115d901cd85127c))
* **languages:** enforce one per-repo budget across clone and API paths ([6888088](https://github.com/twangodev/gmetrics/commit/6888088fbb16e3b8e58d8d349b6b3396a9eb7f7c))
* **languages:** extend per-repo budget to fold and head-resolve paths ([72f2675](https://github.com/twangodev/gmetrics/commit/72f2675ebf69f90de6b11cf566b45440d0da92cc))
* **languages:** skip merge commits in compare-fold for recompute parity ([bb8cd4f](https://github.com/twangodev/gmetrics/commit/bb8cd4fa91033bceaea89976c5aac64dc3b5d4c0))


### Performance Improvements

* **languages:** collapse per-predicate Search into one query ([728ea85](https://github.com/twangodev/gmetrics/commit/728ea85fe4cd959b3688bde9c1c1f2bf86a84659))

## [1.1.0](https://github.com/twangodev/gmetrics/compare/v1.0.1...v1.1.0) (2026-05-26)


### Features

* **languages:** include contributed-to repos via hybrid clone/API path ([dcecf08](https://github.com/twangodev/gmetrics/commit/dcecf08bbe4302bf5ca9a290d643b11136d8fb09))

## [1.0.1](https://github.com/twangodev/gmetrics/compare/v1.0.0...v1.0.1) (2026-05-24)


### Bug Fixes

* **ci:** coerce release_created to boolean literal for docker tag enable ([ac1af46](https://github.com/twangodev/gmetrics/commit/ac1af46e5be369e573c4ff8498eba0a387496255))
* **ci:** use softprops/action-gh-release for asset upload ([112749f](https://github.com/twangodev/gmetrics/commit/112749fa8d54ad8c8f890e2c9838a85d90379445))

## 1.0.0 (2026-05-24)


### Features

* **action:** docker image, action.yml, and CI workflow ([dac941e](https://github.com/twangodev/gmetrics/commit/dac941eb78eda29227e4771a27c83f14d724efd6))
* **base:** authored commit count via Search API ([8a692a6](https://github.com/twangodev/gmetrics/commit/8a692a6d9e677bf646cb05d2314f0ec59fda49d7))
* **base:** expanded data model for activity, community and repository stats ([d91c574](https://github.com/twangodev/gmetrics/commit/d91c5740deda4a0699f33ad71b439c75c9bd9398))
* **base:** full header, two-column sections and hireable pill ([415a175](https://github.com/twangodev/gmetrics/commit/415a1751dff319e8ed4ca0878fa98d69c70286f5))
* **base:** GraphQL fetch for profile, activity, community and repository stats ([9802235](https://github.com/twangodev/gmetrics/commit/9802235fd85ff3cc019a9f19643653210634e1a8))
* **base:** standalone metadata footer fragment ([6c5e507](https://github.com/twangodev/gmetrics/commit/6c5e507e34f5fcec59458779355acb489fe1d385))
* **cli:** render subcommand wiring config → engine → svg file ([2b4c4bd](https://github.com/twangodev/gmetrics/commit/2b4c4bd4526cfe7382b74c592f0e29f307649749))
* **config:** yaml + INPUT_* env loader with typed schema and v1 validation ([09b299d](https://github.com/twangodev/gmetrics/commit/09b299dfb713bb0418c0d5210312217ba8a6b7ee))
* **githubapi:** rest + graphql clients with quota check ([48055e3](https://github.com/twangodev/gmetrics/commit/48055e3d1a04c01dc798de3bd0db6ea8a8193ca8))
* **httpx:** retryable http client with token-bucket rate limiter ([e169c4c](https://github.com/twangodev/gmetrics/commit/e169c4ca32d3c84855cbda89ef347d67c3d0d6dd))
* **languages:** authorship-aware indepth analyzer via native git and numstat ([7603c43](https://github.com/twangodev/gmetrics/commit/7603c4308e33b5017fb0dca5f3f991935bf5a57f))
* **languages:** auto-include GPG-bound emails in commits_authoring ([e6771c2](https://github.com/twangodev/gmetrics/commit/e6771c203a093e3be974cd06abfb630308770a58))
* **languages:** code octicon header and indepth caption ([cfcb1d4](https://github.com/twangodev/gmetrics/commit/cfcb1d4fbea147fc9df609ad03cbec1543f92010))
* **languages:** config, data model, palette and gogs/git-module dep ([e6dd04a](https://github.com/twangodev/gmetrics/commit/e6dd04ad6aaa162b14310a7a61eff28aaec25cbf))
* **log:** add slog logger with tint dev handler ([d079aa4](https://github.com/twangodev/gmetrics/commit/d079aa425c1df761ceea299930a77073b797a714))
* **metrics:** engine — base first, parallel fetch, error isolation ([42966fc](https://github.com/twangodev/gmetrics/commit/42966fc4b35d1dd2c727a56c32cc27986513581b))
* **metrics:** plugin lifecycle, parallel fetch and metadata footer wiring ([09e6bb5](https://github.com/twangodev/gmetrics/commit/09e6bb575d757464e01381bd226a013e79cac972))
* **music:** last.fm recently played card with music-note icon ([9e9202f](https://github.com/twangodev/gmetrics/commit/9e9202f2b415c5cbadca5d16adb624e19514ea20))
* **people:** followers and following sections with people octicon ([0bbd243](https://github.com/twangodev/gmetrics/commit/0bbd243713e3bf2b289b7bbd0caf86e90ba1c582))
* **plugin:** define Plugin interface, Fragment, registry, and error fragment ([edba72f](https://github.com/twangodev/gmetrics/commit/edba72ff5f5e3a4d2126de5243bcfdc7c4e2f0b9))
* **plugins/base:** header, activity, community, repositories, metadata ([ed69b5d](https://github.com/twangodev/gmetrics/commit/ed69b5d927d147f068bfe829e7270403e4718ea7))
* **plugins/languages:** most-used languages with optional indepth mode ([f03fa31](https://github.com/twangodev/gmetrics/commit/f03fa31ec0fe6530214645f3ee3dab441083fdc7))
* **plugins/music:** last.fm recent-tracks renderer ([4f27d81](https://github.com/twangodev/gmetrics/commit/4f27d81245168e8a97c8267f3993c46e4a36bdd3))
* **plugins/people:** followers + following avatar grid ([530f294](https://github.com/twangodev/gmetrics/commit/530f294d5baa49a62198cc09ad118dc27236f522))
* **plugins/steam:** player, most-played, recently-played sections ([d6c9ec0](https://github.com/twangodev/gmetrics/commit/d6c9ec070f33fe5a40dd6c9fbb2b14e33a53b7b4))
* **plugins/wakatime:** time + projects/langs/editors/os bar charts ([63fdced](https://github.com/twangodev/gmetrics/commit/63fdced3151d342dfbfb0537adf0897fcd34c4d6))
* **render:** canvas-based svg frame with bundled Inter font ([aa0abb7](https://github.com/twangodev/gmetrics/commit/aa0abb7e7659b5c62a9e9e70ba05edb098231acc))
* **render:** light-only classic CSS theme and remove frame border ([70380f0](https://github.com/twangodev/gmetrics/commit/70380f08b506e297c7cf33b4ebfbe2a6fc0e3946))
* **render:** octicons library with evenodd fill-rule ([b101f78](https://github.com/twangodev/gmetrics/commit/b101f78983ece7d00fabbcd74f8ab802dde8937a))
* **render:** pt→px font sizing in canvas ([dfc877e](https://github.com/twangodev/gmetrics/commit/dfc877e373e9f7b7b4bc1c4f99f3289b39b7c696))
* **render:** sanitize malformed path data from font glyph C commands ([cd4ef9c](https://github.com/twangodev/gmetrics/commit/cd4ef9c3cbe08dba6aa2ca777c60961ea2382ca6))
* **render:** text-as-path emission helpers ([be50640](https://github.com/twangodev/gmetrics/commit/be506401b582abc93a4dbdb931792cfa145af9f3))
* **steam:** player block and per-game cards ([b751ef0](https://github.com/twangodev/gmetrics/commit/b751ef090e2a598059e2aa8e6831f53d543b7942))
* **wakatime:** prose summary and per-language bar charts ([205d3f0](https://github.com/twangodev/gmetrics/commit/205d3f09902f6a1e7efdfba596371a0465c06cea))


### Bug Fixes

* **release:** seed manifest at 0.0.0 so first release is 0.1.0 ([107b3a9](https://github.com/twangodev/gmetrics/commit/107b3a9b778d9f1b249fbbc35a607f046fcf0946))
