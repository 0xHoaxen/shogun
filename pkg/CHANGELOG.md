# Changelog

## [0.2.0](https://github.com/0xHoaxen/shogun/compare/pkg/v0.1.0...pkg/v0.2.0) (2026-10-10)


### Features

* **bus:** add consumer management and relay functionality ([a9e02b5](https://github.com/0xHoaxen/shogun/commit/a9e02b50b054262aaa526f003934d282b16cf797))
* **bus:** add consumer management and relay functionality ([32bcfaf](https://github.com/0xHoaxen/shogun/commit/32bcfaf1fc4c58552334ee6a0c34b07e227700dc))
* **deploy:** manifests and tooling for a cluster deploy (P10.5a-d) ([ea1878a](https://github.com/0xHoaxen/shogun/commit/ea1878a1e55256c89572598ec80a3841ca80aa94))
* **fude:** add draft store, use cases and gRPC handlers ([64120e7](https://github.com/0xHoaxen/shogun/commit/64120e726e6c32f74d7255d3795ebaca6e944bd1))
* **fude:** approve a draft and stamp its Hanko ([742873d](https://github.com/0xHoaxen/shogun/commit/742873d1f5575e5b1390596bff58c6f1dee0c69f))
* **fude:** draft cover letters and outreach from job and contact events ([437fa1c](https://github.com/0xHoaxen/shogun/commit/437fa1ca2ae9c7b517822bed3735e7778baf9b82))
* **fude:** draft posts from dojo learning events ([04cff9d](https://github.com/0xHoaxen/shogun/commit/04cff9da6407d3a914978e34451953dab1879e95))
* **fude:** drafts, hanko approval and tsubame mail (P7) ([7d4ad95](https://github.com/0xHoaxen/shogun/commit/7d4ad95d9e72d6c118755925a3ed5bd131a65722))
* **fude:** mark an approved copy-only draft as posted ([3b87796](https://github.com/0xHoaxen/shogun/commit/3b87796f8f523b801da8bfdfff46d52fd897a5fd))
* **fude:** point Claude and Gmail clients at a stub for e2e ([d7b6673](https://github.com/0xHoaxen/shogun/commit/d7b66735c39cc4c9645ccc3805531f9ec7b89afc))
* **fude:** send approved email and follow what tsubame reports ([325da3c](https://github.com/0xHoaxen/shogun/commit/325da3c7a5a004294ca4b75dacd6f87e073e5e52))
* **kagami:** apply mail and sent-draft events to jobs and contacts ([4a9fc0c](https://github.com/0xHoaxen/shogun/commit/4a9fc0c9bad6f7fef90bffd5848364287dd8847f))
* **katana:** list, accept and dismiss suggestions, and notify the owner ([9dd3322](https://github.com/0xHoaxen/shogun/commit/9dd3322af50c3102aea54a22197bf6c95f185f16))
* **katana:** suggest profile edits from GitHub changes and finished items ([691787a](https://github.com/0xHoaxen/shogun/commit/691787ac08388a2dd685809ed1b0b95aba3ce59d))
* phase 10 hardening and operations ([297b946](https://github.com/0xHoaxen/shogun/commit/297b9468a391faeb219e1a256ab6c789960618c8))
* phase 9 (dojo, katana, shinobi, sensei) ([f6dc0f3](https://github.com/0xHoaxen/shogun/commit/f6dc0f3f540bbe6f65a5f722fb9f4b0f667f548a))
* phrase 4 ([c59ef02](https://github.com/0xHoaxen/shogun/commit/c59ef02dcb2608ba09198b20b407235ba89b9774))
* **pkg:** add authz and grpcclient packages ([9992346](https://github.com/0xHoaxen/shogun/commit/999234655ecbc27ec43250b38b52c6339a8e2cee))
* **pkg:** add bus interface and routes ([2049b17](https://github.com/0xHoaxen/shogun/commit/2049b17881a190455753a10addb2bbd9fbb71597))
* **pkg:** add event sink and grpc bus ([61d217a](https://github.com/0xHoaxen/shogun/commit/61d217afa63a2075d27021bd8c4a605034dcbe7e))
* **pkg:** add hanko token package ([b159c67](https://github.com/0xHoaxen/shogun/commit/b159c67a12c2bbcddec3a327c65b35f29c98af27))
* **pkg:** add llm, the metered path to Claude ([503dd94](https://github.com/0xHoaxen/shogun/commit/503dd94f7c7df00ecfc38c0d421a682185364ae5))
* **pkg:** add outbox and inbox packages ([9dad3f5](https://github.com/0xHoaxen/shogun/commit/9dad3f50ddb249b9d880a6783b51353b35022fbf))
* **pkg:** add outbox relay ([b3fcafa](https://github.com/0xHoaxen/shogun/commit/b3fcafa3a3e9b5fda6dabda977f8c38a53743454))
* **pkg:** add postgres package ([e1f0d5f](https://github.com/0xHoaxen/shogun/commit/e1f0d5f4bbf23cd650fc97339dad1ac7fcc948b5))
* **pkg:** add server package ([5863b8b](https://github.com/0xHoaxen/shogun/commit/5863b8b37899848303ed00debcbb925e3bbb8bfd))
* **pkg:** add telemetry package ([29ee751](https://github.com/0xHoaxen/shogun/commit/29ee751b1ccb4932391ceca657b0929c6c8c0cc1))
* **pkg:** add version, config and logger packages ([61203f8](https://github.com/0xHoaxen/shogun/commit/61203f8dac6a2631aafa05448ff43de373b3e751))
* **pkg:** add version, config and logger packages ([4a5f242](https://github.com/0xHoaxen/shogun/commit/4a5f242a04a6f84fc63b42c792efce45a373218c))
* **pkg:** let services run their own jobs on the relay's River client ([293ccae](https://github.com/0xHoaxen/shogun/commit/293ccae261f761d19bb14319f9bc410dd0547bb3))
* **pkg:** record traceparent in outbox events ([34f8940](https://github.com/0xHoaxen/shogun/commit/34f89407ab844fec798a603bd29c843cdea7838c))
* **pkg:** service metrics for RPCs, outbox lag, Gmail sync and budgets ([971e432](https://github.com/0xHoaxen/shogun/commit/971e43271094195ca2b9e1c7aedbf386ec2b2966))
* **sensei:** project events into facts ([671fde7](https://github.com/0xHoaxen/shogun/commit/671fde70dba52da7ccba65e02b7e479d4234cdf7))
* **shinobi:** score postings and announce matches ([35cb2b1](https://github.com/0xHoaxen/shogun/commit/35cb2b13fd45c38c198e9726a2f28028c704be75))
* **soroban:** cost control, pkg/llm and spend screen (P6) ([73b7602](https://github.com/0xHoaxen/shogun/commit/73b7602bd2f31b4e47061ad321a23e75d0ad5da4))
* **taiko:** notification settings and a shared daily schedule ([a3ff8c5](https://github.com/0xHoaxen/shogun/commit/a3ff8c542d91be542445a29ec68083961c6d850b))
* **taiko:** notifications, live stream and daily digest (Phase 8) ([49f755e](https://github.com/0xHoaxen/shogun/commit/49f755e9871e5575981054caefe17890ed754d63))
* **taiko:** turn domain events into notifications ([a6e630f](https://github.com/0xHoaxen/shogun/commit/a6e630f480bea2f0f820fd4ad6f9b1b14151d02c))
* **web:** add the drafts queue and review screens ([8faa8ad](https://github.com/0xHoaxen/shogun/commit/8faa8ad09b3ff5c1fb391ea3058cb438b24294dd))


### Bug Fixes

* add the go.sum entries that go work sync left out ([34966ea](https://github.com/0xHoaxen/shogun/commit/34966eaaa32395f5aaf6bae624a4203d5d95c1c1))
* **pkg:** put extensions schema on service search_path ([b69fe74](https://github.com/0xHoaxen/shogun/commit/b69fe740fa0dfc59459284416d3b32a368f11f9f))
* **pkg:** put extensions schema on service search_path ([4a34d9b](https://github.com/0xHoaxen/shogun/commit/4a34d9b933c184866b6f7592803448763174a4d8))
* **pkg:** stop the relay's listener goroutine racing with Stop ([b2d8d3c](https://github.com/0xHoaxen/shogun/commit/b2d8d3c7c762ea31aa61cac047126cfbc6f05bde))

## 1.0.0 (2026-10-02)


### Features

* **bus:** add consumer management and relay functionality ([a9e02b5](https://github.com/0xHoaxen/shogun/commit/a9e02b50b054262aaa526f003934d282b16cf797))
* **bus:** add consumer management and relay functionality ([32bcfaf](https://github.com/0xHoaxen/shogun/commit/32bcfaf1fc4c58552334ee6a0c34b07e227700dc))
* **pkg:** add authz and grpcclient packages ([9992346](https://github.com/0xHoaxen/shogun/commit/999234655ecbc27ec43250b38b52c6339a8e2cee))
* **pkg:** add bus interface and routes ([2049b17](https://github.com/0xHoaxen/shogun/commit/2049b17881a190455753a10addb2bbd9fbb71597))
* **pkg:** add event sink and grpc bus ([61d217a](https://github.com/0xHoaxen/shogun/commit/61d217afa63a2075d27021bd8c4a605034dcbe7e))
* **pkg:** add hanko token package ([b159c67](https://github.com/0xHoaxen/shogun/commit/b159c67a12c2bbcddec3a327c65b35f29c98af27))
* **pkg:** add outbox and inbox packages ([9dad3f5](https://github.com/0xHoaxen/shogun/commit/9dad3f50ddb249b9d880a6783b51353b35022fbf))
* **pkg:** add outbox relay ([b3fcafa](https://github.com/0xHoaxen/shogun/commit/b3fcafa3a3e9b5fda6dabda977f8c38a53743454))
* **pkg:** add postgres package ([e1f0d5f](https://github.com/0xHoaxen/shogun/commit/e1f0d5f4bbf23cd650fc97339dad1ac7fcc948b5))
* **pkg:** add server package ([5863b8b](https://github.com/0xHoaxen/shogun/commit/5863b8b37899848303ed00debcbb925e3bbb8bfd))
* **pkg:** add telemetry package ([29ee751](https://github.com/0xHoaxen/shogun/commit/29ee751b1ccb4932391ceca657b0929c6c8c0cc1))
* **pkg:** add version, config and logger packages ([61203f8](https://github.com/0xHoaxen/shogun/commit/61203f8dac6a2631aafa05448ff43de373b3e751))
* **pkg:** add version, config and logger packages ([4a5f242](https://github.com/0xHoaxen/shogun/commit/4a5f242a04a6f84fc63b42c792efce45a373218c))
* **pkg:** record traceparent in outbox events ([34f8940](https://github.com/0xHoaxen/shogun/commit/34f89407ab844fec798a603bd29c843cdea7838c))


### Bug Fixes

* **pkg:** put extensions schema on service search_path ([b69fe74](https://github.com/0xHoaxen/shogun/commit/b69fe740fa0dfc59459284416d3b32a368f11f9f))
* **pkg:** put extensions schema on service search_path ([4a34d9b](https://github.com/0xHoaxen/shogun/commit/4a34d9b933c184866b6f7592803448763174a4d8))
