# Changelog

## [1.2.0](https://github.com/0xHoaxen/shogun/compare/services/katana/v1.1.0...services/katana/v1.2.0) (2026-10-23)


### Features

* **llm:** call providers through the OpenAI chat completions format ([7fb6276](https://github.com/0xHoaxen/shogun/commit/7fb6276d34081efd46616aef8e1919639e5934e9))
* **llm:** call providers through the OpenAI chat completions format ([cb399fd](https://github.com/0xHoaxen/shogun/commit/cb399fd8aff1ce86def6e8622604ea404396692d))


### Bug Fixes

* bump Go to 1.26.10 for GO-2026-6809 ([27b914c](https://github.com/0xHoaxen/shogun/commit/27b914ce8990401048db4dfccf7b70ba5677f3aa))
* **web:** bump next to 16.4.2 for critical advisories ([03b1072](https://github.com/0xHoaxen/shogun/commit/03b10721c195e446411756ebd56bee830f2e33ef))

## [1.1.0](https://github.com/0xHoaxen/shogun/compare/services/katana/v1.0.0...services/katana/v1.1.0) (2026-10-20)


### Features

* **kagami:** scan each owner's follow-ups on their own clock ([fe868aa](https://github.com/0xHoaxen/shogun/commit/fe868aa0208b0be1d73a7a5289a11e196c1bf1ae))
* **katana:** connect a user's own GitHub through OAuth ([6bf10dc](https://github.com/0xHoaxen/shogun/commit/6bf10dc012c076ed2e20db79915202747bae25a4))
* **katana:** purge a deleted user's snapshots, suggestions and GitHub token ([5a4c248](https://github.com/0xHoaxen/shogun/commit/5a4c248e1cc71e3e81476836af4417e1ab345923))
* **katana:** store each user's own sealed GitHub token and sync with it ([ae2df8a](https://github.com/0xHoaxen/shogun/commit/ae2df8ac453fae045f2e5758fffe0cc597515fab))
* **llm:** call each user's own provider and drop the shared Anthropic settings ([7d1ef65](https://github.com/0xHoaxen/shogun/commit/7d1ef65cb0319da08d4040b2aced3aa9fe413863))
* multi-user phase 11 (sign-up, per-user providers, account deletion) ([1517322](https://github.com/0xHoaxen/shogun/commit/1517322d2b8015be13d7dd5ff37e0a8b73a0d6d5))
* **shinobi:** read each owner's source schedules in their own time zone ([91a4c48](https://github.com/0xHoaxen/shogun/commit/91a4c48b4c482e2225beaaeea828e96759eb4a01))
* **torii:** announce users to services and keep a local owners list ([10ffeb1](https://github.com/0xHoaxen/shogun/commit/10ffeb1a2fd0671448dbf2fbec7b559073f481dc))


### Bug Fixes

* **katana:** stop sharing one GitHub account across users and sync on each owner's clock ([9bbacbf](https://github.com/0xHoaxen/shogun/commit/9bbacbf6f2917dd1f5e5970d3a755ec10d583cb5))

## [1.0.0](https://github.com/0xHoaxen/shogun/compare/services/katana/v0.0.1...services/katana/v1.0.0) (2026-10-10)


### Features

* **bus:** add consumer management and relay functionality ([a9e02b5](https://github.com/0xHoaxen/shogun/commit/a9e02b50b054262aaa526f003934d282b16cf797))
* **bus:** add consumer management and relay functionality ([32bcfaf](https://github.com/0xHoaxen/shogun/commit/32bcfaf1fc4c58552334ee6a0c34b07e227700dc))
* **deploy:** add migrate subcommand and enable migration Job ([12f9f8d](https://github.com/0xHoaxen/shogun/commit/12f9f8d5a1f5f24ae0a4d6507ded47f5aa493939))
* **deploy:** manifests and tooling for a cluster deploy (P10.5a-d) ([ea1878a](https://github.com/0xHoaxen/shogun/commit/ea1878a1e55256c89572598ec80a3841ca80aa94))
* **katana:** list, accept and dismiss suggestions, and notify the owner ([9dd3322](https://github.com/0xHoaxen/shogun/commit/9dd3322af50c3102aea54a22197bf6c95f185f16))
* **katana:** suggest profile edits from GitHub changes and finished items ([691787a](https://github.com/0xHoaxen/shogun/commit/691787ac08388a2dd685809ed1b0b95aba3ce59d))
* **katana:** suggestion and snapshot tables, protos, domain and store ([1959208](https://github.com/0xHoaxen/shogun/commit/1959208143ceb453b0dae55a72fad4c349d0fc7d))
* **katana:** sync GitHub on a schedule and on demand ([2268fc4](https://github.com/0xHoaxen/shogun/commit/2268fc4d2807126f27eed58b7a5b01602508981d))
* phase 10 hardening and operations ([297b946](https://github.com/0xHoaxen/shogun/commit/297b9468a391faeb219e1a256ab6c789960618c8))
* phase 9 (dojo, katana, shinobi, sensei) ([f6dc0f3](https://github.com/0xHoaxen/shogun/commit/f6dc0f3f540bbe6f65a5f722fb9f4b0f667f548a))
* **repo:** generate the ten service skeletons ([25b1e55](https://github.com/0xHoaxen/shogun/commit/25b1e554d8c371bff3266870d65e0dbde6f2d40b))
* service skeletons ([c626699](https://github.com/0xHoaxen/shogun/commit/c6266991fc34d4c73b838ea046f5bd778711f0d0))


### Bug Fixes

* add the go.sum entries that go work sync left out ([34966ea](https://github.com/0xHoaxen/shogun/commit/34966eaaa32395f5aaf6bae624a4203d5d95c1c1))

## Changelog
