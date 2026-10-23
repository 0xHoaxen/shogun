# Changelog

## [1.2.0](https://github.com/0xHoaxen/shogun/compare/services/tsubame/v1.1.0...services/tsubame/v1.2.0) (2026-10-23)


### Features

* **llm:** call providers through the OpenAI chat completions format ([7fb6276](https://github.com/0xHoaxen/shogun/commit/7fb6276d34081efd46616aef8e1919639e5934e9))
* **llm:** call providers through the OpenAI chat completions format ([cb399fd](https://github.com/0xHoaxen/shogun/commit/cb399fd8aff1ce86def6e8622604ea404396692d))


### Bug Fixes

* bump Go to 1.26.10 for GO-2026-6809 ([27b914c](https://github.com/0xHoaxen/shogun/commit/27b914ce8990401048db4dfccf7b70ba5677f3aa))
* **web:** bump next to 16.4.2 for critical advisories ([03b1072](https://github.com/0xHoaxen/shogun/commit/03b10721c195e446411756ebd56bee830f2e33ef))

## [1.1.0](https://github.com/0xHoaxen/shogun/compare/services/tsubame/v1.0.0...services/tsubame/v1.1.0) (2026-10-20)


### Features

* **llm:** call each user's own provider and drop the shared Anthropic settings ([7d1ef65](https://github.com/0xHoaxen/shogun/commit/7d1ef65cb0319da08d4040b2aced3aa9fe413863))
* multi-user phase 11 (sign-up, per-user providers, account deletion) ([1517322](https://github.com/0xHoaxen/shogun/commit/1517322d2b8015be13d7dd5ff37e0a8b73a0d6d5))
* **root:** guard sqlc queries with an owner-scope check ([930c495](https://github.com/0xHoaxen/shogun/commit/930c495772179f1d41ef0d5f72ee9958aedd8b62))

## [1.0.0](https://github.com/0xHoaxen/shogun/compare/services/tsubame/v0.0.1...services/tsubame/v1.0.0) (2026-10-10)


### Features

* **bus:** add consumer management and relay functionality ([a9e02b5](https://github.com/0xHoaxen/shogun/commit/a9e02b50b054262aaa526f003934d282b16cf797))
* **bus:** add consumer management and relay functionality ([32bcfaf](https://github.com/0xHoaxen/shogun/commit/32bcfaf1fc4c58552334ee6a0c34b07e227700dc))
* **deploy:** add migrate subcommand and enable migration Job ([12f9f8d](https://github.com/0xHoaxen/shogun/commit/12f9f8d5a1f5f24ae0a4d6507ded47f5aa493939))
* **deploy:** manifests and tooling for a cluster deploy (P10.5a-d) ([ea1878a](https://github.com/0xHoaxen/shogun/commit/ea1878a1e55256c89572598ec80a3841ca80aa94))
* **fude:** drafts, hanko approval and tsubame mail (P7) ([7d4ad95](https://github.com/0xHoaxen/shogun/commit/7d4ad95d9e72d6c118755925a3ed5bd131a65722))
* **fude:** mark an approved copy-only draft as posted ([3b87796](https://github.com/0xHoaxen/shogun/commit/3b87796f8f523b801da8bfdfff46d52fd897a5fd))
* **fude:** point Claude and Gmail clients at a stub for e2e ([d7b6673](https://github.com/0xHoaxen/shogun/commit/d7b66735c39cc4c9645ccc3805531f9ec7b89afc))
* phase 10 hardening and operations ([297b946](https://github.com/0xHoaxen/shogun/commit/297b9468a391faeb219e1a256ab6c789960618c8))
* **pkg:** service metrics for RPCs, outbox lag, Gmail sync and budgets ([971e432](https://github.com/0xHoaxen/shogun/commit/971e43271094195ca2b9e1c7aedbf386ec2b2966))
* **repo:** generate the ten service skeletons ([25b1e55](https://github.com/0xHoaxen/shogun/commit/25b1e554d8c371bff3266870d65e0dbde6f2d40b))
* service skeletons ([c626699](https://github.com/0xHoaxen/shogun/commit/c6266991fc34d4c73b838ea046f5bd778711f0d0))
* **tsubame:** add mail tables and encrypted account storage ([98729d3](https://github.com/0xHoaxen/shogun/commit/98729d344b56a6737853bad2c9b0d11fd562cdb5))
* **tsubame:** add the mail provider seam and a Gmail client ([7e19b0d](https://github.com/0xHoaxen/shogun/commit/7e19b0d6876a3d21f4dc4f22815d385f118cd0dd))
* **tsubame:** classify synced mail and link it to jobs and contacts ([589bbf3](https://github.com/0xHoaxen/shogun/commit/589bbf3f09131ca08881ed5b4ce52dfe7a29c9d9))
* **tsubame:** connect a Gmail account with OAuth ([02efd53](https://github.com/0xHoaxen/shogun/commit/02efd531199a360401bc4fa8dcc0ebed4a2c9364))
* **tsubame:** reconcile sends left half-finished ([6b26e7b](https://github.com/0xHoaxen/shogun/commit/6b26e7ba6b68318c42095135a087c9961e5e740d))
* **tsubame:** send approved drafts only with a valid Hanko ([cf472fa](https://github.com/0xHoaxen/shogun/commit/cf472fad589b3d2bb0c52f04242f2cc566c9edd5))
* **tsubame:** sync Gmail on a schedule ([d770094](https://github.com/0xHoaxen/shogun/commit/d770094b0d148828ca33e982c2725a750b533e99))


### Bug Fixes

* add the go.sum entries that go work sync left out ([34966ea](https://github.com/0xHoaxen/shogun/commit/34966eaaa32395f5aaf6bae624a4203d5d95c1c1))

## Changelog
