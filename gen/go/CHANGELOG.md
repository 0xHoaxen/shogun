# Changelog

## [1.3.0](https://github.com/0xHoaxen/shogun/compare/gen/go/v1.2.0...gen/go/v1.3.0) (2026-10-23)


### Features

* **kagami:** map any contacts sheet onto the import columns ([560a1e5](https://github.com/0xHoaxen/shogun/commit/560a1e5a19a4f4fd62ec8ddacb939164577a5d95))
* **kagami:** reshape contacts sheets with the owner's AI provider ([ed33cdf](https://github.com/0xHoaxen/shogun/commit/ed33cdf835f364b41d251129f4be53060e6febec))
* **kagami:** reshape contacts sheets with the owner's AI provider ([552e912](https://github.com/0xHoaxen/shogun/commit/552e91205674e0dc3d166663163ddeeafa5e02af))
* **llm:** call providers through the OpenAI chat completions format ([7fb6276](https://github.com/0xHoaxen/shogun/commit/7fb6276d34081efd46616aef8e1919639e5934e9))
* **llm:** call providers through the OpenAI chat completions format ([cb399fd](https://github.com/0xHoaxen/shogun/commit/cb399fd8aff1ce86def6e8622604ea404396692d))
* **torii:** pass contacts sheet reshaping through to kagami ([ede185f](https://github.com/0xHoaxen/shogun/commit/ede185f1ce5a07481371f1c61efed23cb7940386))
* **torii:** pass contacts sheet reshaping through to kagami ([33b24a7](https://github.com/0xHoaxen/shogun/commit/33b24a788f6bef93687fbb4db6da099288ce9860))


### Bug Fixes

* bump Go to 1.26.10 for GO-2026-6809 ([27b914c](https://github.com/0xHoaxen/shogun/commit/27b914ce8990401048db4dfccf7b70ba5677f3aa))
* **web:** bump next to 16.4.2 for critical advisories ([03b1072](https://github.com/0xHoaxen/shogun/commit/03b10721c195e446411756ebd56bee830f2e33ef))

## [1.2.0](https://github.com/0xHoaxen/shogun/compare/gen/go/v1.1.0...gen/go/v1.2.0) (2026-10-20)


### Features

* **fude:** test a user's AI provider with one small metered call ([64cf060](https://github.com/0xHoaxen/shogun/commit/64cf060f26dd65455dca73bb86619dee76be819f))
* **katana:** connect a user's own GitHub through OAuth ([6bf10dc](https://github.com/0xHoaxen/shogun/commit/6bf10dc012c076ed2e20db79915202747bae25a4))
* multi-user phase 11 (sign-up, per-user providers, account deletion) ([1517322](https://github.com/0xHoaxen/shogun/commit/1517322d2b8015be13d7dd5ff37e0a8b73a0d6d5))
* **soroban:** optional monthly token cap per user ([97fdb4c](https://github.com/0xHoaxen/shogun/commit/97fdb4ce6132a84a7707d44b7ab5f6d75ce312c1))
* **soroban:** reserve against the caller's own provider and hand it back ([80be81f](https://github.com/0xHoaxen/shogun/commit/80be81f52b6d85c474f8f7eb3c5f41f52d1a3ca9))
* **soroban:** store each user's AI provider with a sealed key ([5563e98](https://github.com/0xHoaxen/shogun/commit/5563e98c513dea0d018f3aa5142ae0ba52c78f90))
* **torii:** admin RPCs, Gmail approval gate and Users page ([491da82](https://github.com/0xHoaxen/shogun/commit/491da8210bdb3764d36222456f20cdaf96679629))
* **torii:** AI provider API that never returns or logs the key ([b2d1e8c](https://github.com/0xHoaxen/shogun/commit/b2d1e8c77911cdc74de62cb3dac0ebb80ce609e8))
* **torii:** announce users to services and keep a local owners list ([10ffeb1](https://github.com/0xHoaxen/shogun/commit/10ffeb1a2fd0671448dbf2fbec7b559073f481dc))
* **torii:** connect GitHub from Settings ([b6ccefb](https://github.com/0xHoaxen/shogun/commit/b6ccefb54b3abf7c6f5476b393d11de9ba359ec0))
* **torii:** delete an account and announce it with user.deleted ([7768244](https://github.com/0xHoaxen/shogun/commit/776824413c62a69ddb8442d744c382a42bb98f10))
* **torii:** set a time zone and sign out everywhere from Settings ([26efebd](https://github.com/0xHoaxen/shogun/commit/26efebd3fcc80e28f15080d806aa5a25e02008d8))

## [1.1.0](https://github.com/0xHoaxen/shogun/compare/gen/go/v1.0.0...gen/go/v1.1.0) (2026-10-15)


### Features

* **shinobi:** delete a discovery source ([e5407bd](https://github.com/0xHoaxen/shogun/commit/e5407bd71030c254cf50bdd9c00585ea05f64475))
* **shinobi:** delete a discovery source ([ac6101d](https://github.com/0xHoaxen/shogun/commit/ac6101d08d9d34f92e1740711c603a81c95474bf))
* **shinobi:** say when a source's address is a web page ([41640cf](https://github.com/0xHoaxen/shogun/commit/41640cf2e59fcd2194610cf9fe8048ce3fbafd37))


### Bug Fixes

* **web:** draw select chevron inside the field padding ([7d6333a](https://github.com/0xHoaxen/shogun/commit/7d6333aa89a88d52556ccc0190a107495b8d5197))

## [1.0.0](https://github.com/0xHoaxen/shogun/compare/gen/go/v0.0.1...gen/go/v1.0.0) (2026-10-10)


### Features

* **deploy:** manifests and tooling for a cluster deploy (P10.5a-d) ([ea1878a](https://github.com/0xHoaxen/shogun/commit/ea1878a1e55256c89572598ec80a3841ca80aa94))
* **dojo:** learning tables, protos, domain and store ([cb4a391](https://github.com/0xHoaxen/shogun/commit/cb4a3914d17077866d8faebb7750f6bc14628b24))
* **fude,soroban:** name the owner in draft and cost events ([6b0ef60](https://github.com/0xHoaxen/shogun/commit/6b0ef60be98f4fcfee0b3e33dcfa684a7c004c12))
* **fude:** add draft tables, proto and state machine ([d411a9a](https://github.com/0xHoaxen/shogun/commit/d411a9ae623e0e18ed4bae7a3094ff453fdb3719))
* **fude:** drafts, hanko approval and tsubame mail (P7) ([7d4ad95](https://github.com/0xHoaxen/shogun/commit/7d4ad95d9e72d6c118755925a3ed5bd131a65722))
* **fude:** mark an approved copy-only draft as posted ([3b87796](https://github.com/0xHoaxen/shogun/commit/3b87796f8f523b801da8bfdfff46d52fd897a5fd))
* **fude:** mark an approved copy-only draft as posted ([480a4ca](https://github.com/0xHoaxen/shogun/commit/480a4caab97beb0d368cb302dd8d11fe9f2ca417))
* **fude:** show each queued draft's subject and a preview ([b3c5d94](https://github.com/0xHoaxen/shogun/commit/b3c5d94e498bf8c16cba1bb323756d09ed33680d))
* **gen:** generate event envelope go code ([ab629e0](https://github.com/0xHoaxen/shogun/commit/ab629e06fc826ddf7fefc5a5d58d48d1a41df30b))
* **kagami:** add service RPCs and event payload protos ([fbbceb9](https://github.com/0xHoaxen/shogun/commit/fbbceb95f1f1a91b0c477763d7a15d3dc2cf3c9d))
* **kagami:** add service RPCs and event payload protos ([cab836a](https://github.com/0xHoaxen/shogun/commit/cab836af6bd932ac42f8a031bdde905f716d743b))
* **kagami:** find the contact and job a piece of mail is about ([340c244](https://github.com/0xHoaxen/shogun/commit/340c244c211ba4af5d77fad09bed8da0c3e80d12))
* **kagami:** name the owner in follow-up and status events ([12e8ee1](https://github.com/0xHoaxen/shogun/commit/12e8ee11dfc1c0dbc1722127c1fe97ffee4ce9d4))
* **kagami:** name the owner in job.added and contact.status_changed ([14a58bc](https://github.com/0xHoaxen/shogun/commit/14a58bc2fdb2dd7f6f9344ab5237e08566e024e2))
* **kagami:** return company names with jobs and contacts ([fd69c93](https://github.com/0xHoaxen/shogun/commit/fd69c93feffd73c23e0b6a09effc3d2c093d3bf3))
* **katana:** list, accept and dismiss suggestions, and notify the owner ([9dd3322](https://github.com/0xHoaxen/shogun/commit/9dd3322af50c3102aea54a22197bf6c95f185f16))
* **katana:** suggest profile edits from GitHub changes and finished items ([691787a](https://github.com/0xHoaxen/shogun/commit/691787ac08388a2dd685809ed1b0b95aba3ce59d))
* **katana:** suggestion and snapshot tables, protos, domain and store ([1959208](https://github.com/0xHoaxen/shogun/commit/1959208143ceb453b0dae55a72fad4c349d0fc7d))
* phase 10 hardening and operations ([297b946](https://github.com/0xHoaxen/shogun/commit/297b9468a391faeb219e1a256ab6c789960618c8))
* phase 9 (dojo, katana, shinobi, sensei) ([f6dc0f3](https://github.com/0xHoaxen/shogun/commit/f6dc0f3f540bbe6f65a5f722fb9f4b0f667f548a))
* phrase 4 ([c59ef02](https://github.com/0xHoaxen/shogun/commit/c59ef02dcb2608ba09198b20b407235ba89b9774))
* phrase 5 ([3bc189b](https://github.com/0xHoaxen/shogun/commit/3bc189b6d2156ce784011ea784ae030df7d24943))
* **pkg:** add event sink and grpc bus ([61d217a](https://github.com/0xHoaxen/shogun/commit/61d217afa63a2075d27021bd8c4a605034dcbe7e))
* **proto:** add browser api/v1 protos and TS codegen template ([dae1012](https://github.com/0xHoaxen/shogun/commit/dae10120c4af812acc6b68a569ead9a60bb61e90))
* **repo:** generate the ten service skeletons ([25b1e55](https://github.com/0xHoaxen/shogun/commit/25b1e554d8c371bff3266870d65e0dbde6f2d40b))
* **sensei:** nightly rollup and funnel and outreach stats ([2b85703](https://github.com/0xHoaxen/shogun/commit/2b85703f24fcfcfb7a1db697992e7581a1bd11c6))
* **sensei:** project events into facts ([671fde7](https://github.com/0xHoaxen/shogun/commit/671fde70dba52da7ccba65e02b7e479d4234cdf7))
* service skeletons ([c626699](https://github.com/0xHoaxen/shogun/commit/c6266991fc34d4c73b838ea046f5bd778711f0d0))
* **shinobi:** discovery tables, protos, domain rules and store ([c69b2a1](https://github.com/0xHoaxen/shogun/commit/c69b2a157b87d8139c8f3acad6c21a96140bd07c))
* **shinobi:** score postings and announce matches ([35cb2b1](https://github.com/0xHoaxen/shogun/commit/35cb2b13fd45c38c198e9726a2f28028c704be75))
* **soroban:** add cost-control tables, seed and proto ([7250803](https://github.com/0xHoaxen/shogun/commit/72508030e8b9cc1d0b8100af896bb1c71dd67b03))
* **soroban:** cost control, pkg/llm and spend screen (P6) ([73b7602](https://github.com/0xHoaxen/shogun/commit/73b7602bd2f31b4e47061ad321a23e75d0ad5da4))
* **soroban:** update a budget by id and version alone ([5872597](https://github.com/0xHoaxen/shogun/commit/5872597e6b5e7d666b8bf6170aa3ff1ed8e1bd62))
* **taiko:** channel settings RPCs and the notification settings form ([d4a57e4](https://github.com/0xHoaxen/shogun/commit/d4a57e4d8cc45203c61f4f1e6fbca82f4673622f))
* **taiko:** notification settings and a shared daily schedule ([a3ff8c5](https://github.com/0xHoaxen/shogun/commit/a3ff8c542d91be542445a29ec68083961c6d850b))
* **taiko:** notification tables, proto, domain and store ([3afb0f0](https://github.com/0xHoaxen/shogun/commit/3afb0f0333d2d8426a9d7c1252d4598316f06200))
* **taiko:** notifications, live stream and daily digest (Phase 8) ([49f755e](https://github.com/0xHoaxen/shogun/commit/49f755e9871e5575981054caefe17890ed754d63))
* **torii:** add Connect session interceptor and AuthService ([ba72da0](https://github.com/0xHoaxen/shogun/commit/ba72da02bc0aad595ef8812a5f7d96a3c1a09715))
* **torii:** add Costs API backed by soroban ([5f3682d](https://github.com/0xHoaxen/shogun/commit/5f3682d8e9f18dea7ee4e87f386cc0424f95465c))
* **torii:** add the drafts and mail APIs ([3c3ed44](https://github.com/0xHoaxen/shogun/commit/3c3ed44a1f66de7a1cddb12126b6b4e6229b7f92))
* **torii:** learning API over dojo ([381953c](https://github.com/0xHoaxen/shogun/commit/381953cc91b1fb496b459674eb9220df36c63666))
* **torii:** notifications API with a live stream ([9203c37](https://github.com/0xHoaxen/shogun/commit/9203c37577954704b098d4c9368398032b6ef2f5))
* **tsubame:** classify synced mail and link it to jobs and contacts ([589bbf3](https://github.com/0xHoaxen/shogun/commit/589bbf3f09131ca08881ed5b4ce52dfe7a29c9d9))
* **tsubame:** connect a Gmail account with OAuth ([02efd53](https://github.com/0xHoaxen/shogun/commit/02efd531199a360401bc4fa8dcc0ebed4a2c9364))
* **tsubame:** send approved drafts only with a valid Hanko ([cf472fa](https://github.com/0xHoaxen/shogun/commit/cf472fad589b3d2bb0c52f04242f2cc566c9edd5))
* **web:** insights screen over a torii InsightsService ([bd04c27](https://github.com/0xHoaxen/shogun/commit/bd04c2724995e50e1985944aca0c7dc4710a552e))
* **web:** job discovery screen over a torii DiscoveryService ([57e3eb1](https://github.com/0xHoaxen/shogun/commit/57e3eb1750ec7408e755e7c81a360282cf3c1eb7))
* **web:** profile suggestions screen over a torii ProfileService ([52b611d](https://github.com/0xHoaxen/shogun/commit/52b611da12737c2852a3a1d1d73548330b6f0e96))


### Bug Fixes

* add the go.sum entries that go work sync left out ([34966ea](https://github.com/0xHoaxen/shogun/commit/34966eaaa32395f5aaf6bae624a4203d5d95c1c1))

## Changelog
