# Changelog

## [1.2.0](https://github.com/0xHoaxen/shogun/compare/services/torii/v1.1.0...services/torii/v1.2.0) (2026-10-20)


### Features

* **kagami:** scan each owner's follow-ups on their own clock ([fe868aa](https://github.com/0xHoaxen/shogun/commit/fe868aa0208b0be1d73a7a5289a11e196c1bf1ae))
* multi-user phase 11 (sign-up, per-user providers, account deletion) ([1517322](https://github.com/0xHoaxen/shogun/commit/1517322d2b8015be13d7dd5ff37e0a8b73a0d6d5))
* **root:** guard sqlc queries with an owner-scope check ([930c495](https://github.com/0xHoaxen/shogun/commit/930c495772179f1d41ef0d5f72ee9958aedd8b62))
* **torii:** add users table and store ([2d9edd6](https://github.com/0xHoaxen/shogun/commit/2d9edd6166aaecede6056289885cde0ef161d434))
* **torii:** admin RPCs, Gmail approval gate and Users page ([491da82](https://github.com/0xHoaxen/shogun/commit/491da8210bdb3764d36222456f20cdaf96679629))
* **torii:** AI provider API that never returns or logs the key ([b2d1e8c](https://github.com/0xHoaxen/shogun/commit/b2d1e8c77911cdc74de62cb3dac0ebb80ce609e8))
* **torii:** announce users to services and keep a local owners list ([10ffeb1](https://github.com/0xHoaxen/shogun/commit/10ffeb1a2fd0671448dbf2fbec7b559073f481dc))
* **torii:** connect GitHub from Settings ([b6ccefb](https://github.com/0xHoaxen/shogun/commit/b6ccefb54b3abf7c6f5476b393d11de9ba359ec0))
* **torii:** delete an account and announce it with user.deleted ([7768244](https://github.com/0xHoaxen/shogun/commit/776824413c62a69ddb8442d744c382a42bb98f10))
* **torii:** open Google sign-up with admin bootstrap ([bf3b8f7](https://github.com/0xHoaxen/shogun/commit/bf3b8f7dd01e4a5ec27b63bc68b042c6eaae43c2))
* **torii:** rate limit each signed-in user separately ([6a46686](https://github.com/0xHoaxen/shogun/commit/6a46686f46299af314b46f81bd29b606cc129f3b))
* **torii:** set a time zone and sign out everywhere from Settings ([26efebd](https://github.com/0xHoaxen/shogun/commit/26efebd3fcc80e28f15080d806aa5a25e02008d8))

## [1.1.0](https://github.com/0xHoaxen/shogun/compare/services/torii/v1.0.0...services/torii/v1.1.0) (2026-10-15)


### Features

* **shinobi:** delete a discovery source ([e5407bd](https://github.com/0xHoaxen/shogun/commit/e5407bd71030c254cf50bdd9c00585ea05f64475))
* **shinobi:** delete a discovery source ([ac6101d](https://github.com/0xHoaxen/shogun/commit/ac6101d08d9d34f92e1740711c603a81c95474bf))
* **shinobi:** say when a source's address is a web page ([41640cf](https://github.com/0xHoaxen/shogun/commit/41640cf2e59fcd2194610cf9fe8048ce3fbafd37))


### Bug Fixes

* **web:** draw select chevron inside the field padding ([7d6333a](https://github.com/0xHoaxen/shogun/commit/7d6333aa89a88d52556ccc0190a107495b8d5197))

## [1.0.0](https://github.com/0xHoaxen/shogun/compare/services/torii/v0.0.1...services/torii/v1.0.0) (2026-10-10)


### Features

* **bus:** add consumer management and relay functionality ([a9e02b5](https://github.com/0xHoaxen/shogun/commit/a9e02b50b054262aaa526f003934d282b16cf797))
* **bus:** add consumer management and relay functionality ([32bcfaf](https://github.com/0xHoaxen/shogun/commit/32bcfaf1fc4c58552334ee6a0c34b07e227700dc))
* **deploy:** add migrate subcommand and enable migration Job ([12f9f8d](https://github.com/0xHoaxen/shogun/commit/12f9f8d5a1f5f24ae0a4d6507ded47f5aa493939))
* **deploy:** manifests and tooling for a cluster deploy (P10.5a-d) ([ea1878a](https://github.com/0xHoaxen/shogun/commit/ea1878a1e55256c89572598ec80a3841ca80aa94))
* **fude:** drafts, hanko approval and tsubame mail (P7) ([7d4ad95](https://github.com/0xHoaxen/shogun/commit/7d4ad95d9e72d6c118755925a3ed5bd131a65722))
* **fude:** mark an approved copy-only draft as posted ([3b87796](https://github.com/0xHoaxen/shogun/commit/3b87796f8f523b801da8bfdfff46d52fd897a5fd))
* **fude:** mark an approved copy-only draft as posted ([480a4ca](https://github.com/0xHoaxen/shogun/commit/480a4caab97beb0d368cb302dd8d11fe9f2ca417))
* phase 10 hardening and operations ([297b946](https://github.com/0xHoaxen/shogun/commit/297b9468a391faeb219e1a256ab6c789960618c8))
* phase 9 (dojo, katana, shinobi, sensei) ([f6dc0f3](https://github.com/0xHoaxen/shogun/commit/f6dc0f3f540bbe6f65a5f722fb9f4b0f667f548a))
* phrase 5 ([3bc189b](https://github.com/0xHoaxen/shogun/commit/3bc189b6d2156ce784011ea784ae030df7d24943))
* **repo:** generate the ten service skeletons ([25b1e55](https://github.com/0xHoaxen/shogun/commit/25b1e554d8c371bff3266870d65e0dbde6f2d40b))
* service skeletons ([c626699](https://github.com/0xHoaxen/shogun/commit/c6266991fc34d4c73b838ea046f5bd778711f0d0))
* **soroban:** cost control, pkg/llm and spend screen (P6) ([73b7602](https://github.com/0xHoaxen/shogun/commit/73b7602bd2f31b4e47061ad321a23e75d0ad5da4))
* **taiko:** channel settings RPCs and the notification settings form ([d4a57e4](https://github.com/0xHoaxen/shogun/commit/d4a57e4d8cc45203c61f4f1e6fbca82f4673622f))
* **taiko:** notification settings and a shared daily schedule ([a3ff8c5](https://github.com/0xHoaxen/shogun/commit/a3ff8c542d91be542445a29ec68083961c6d850b))
* **taiko:** notifications, live stream and daily digest (Phase 8) ([49f755e](https://github.com/0xHoaxen/shogun/commit/49f755e9871e5575981054caefe17890ed754d63))
* **torii:** add Connect session interceptor and AuthService ([ba72da0](https://github.com/0xHoaxen/shogun/commit/ba72da02bc0aad595ef8812a5f7d96a3c1a09715))
* **torii:** add Contacts API and CSV import backed by kagami ([d2a8948](https://github.com/0xHoaxen/shogun/commit/d2a8948c1d91030e7ecaec293cdfbc5dd0462f62))
* **torii:** add Costs API backed by soroban ([5f3682d](https://github.com/0xHoaxen/shogun/commit/5f3682d8e9f18dea7ee4e87f386cc0424f95465c))
* **torii:** add Google OAuth login flow ([faae376](https://github.com/0xHoaxen/shogun/commit/faae3764347e1f551fc3975c70ed9a2f5c368fd4))
* **torii:** add Jobs API backed by kagami ([0b6271a](https://github.com/0xHoaxen/shogun/commit/0b6271a5b695371cd3e0fb46d4afd7abacd95d41))
* **torii:** add sessions store and auth use cases ([ed5a257](https://github.com/0xHoaxen/shogun/commit/ed5a25731669d745ac753d38015e99f4bb04b5d1))
* **torii:** add the drafts and mail APIs ([3c3ed44](https://github.com/0xHoaxen/shogun/commit/3c3ed44a1f66de7a1cddb12126b6b4e6229b7f92))
* **torii:** learning API over dojo ([381953c](https://github.com/0xHoaxen/shogun/commit/381953cc91b1fb496b459674eb9220df36c63666))
* **torii:** notifications API with a live stream ([9203c37](https://github.com/0xHoaxen/shogun/commit/9203c37577954704b098d4c9368398032b6ef2f5))
* **torii:** serve the browser API on a public listener ([60aa75f](https://github.com/0xHoaxen/shogun/commit/60aa75f22413b41c51f9f320f1dafc9e995b26d2))
* **web:** CSP and security headers, secret and dependency scans, https-only production cookies ([5937306](https://github.com/0xHoaxen/shogun/commit/5937306e9c1e335bc79e083028adf25918bef161))
* **web:** insights screen over a torii InsightsService ([bd04c27](https://github.com/0xHoaxen/shogun/commit/bd04c2724995e50e1985944aca0c7dc4710a552e))
* **web:** job discovery screen over a torii DiscoveryService ([57e3eb1](https://github.com/0xHoaxen/shogun/commit/57e3eb1750ec7408e755e7c81a360282cf3c1eb7))
* **web:** profile suggestions screen over a torii ProfileService ([52b611d](https://github.com/0xHoaxen/shogun/commit/52b611da12737c2852a3a1d1d73548330b6f0e96))


### Bug Fixes

* add the go.sum entries that go work sync left out ([34966ea](https://github.com/0xHoaxen/shogun/commit/34966eaaa32395f5aaf6bae624a4203d5d95c1c1))
* **torii:** authenticate server streams with the session interceptor ([e1d260c](https://github.com/0xHoaxen/shogun/commit/e1d260c7dad85dfd531a03ff2a1cf0129fbff711))

## Changelog
