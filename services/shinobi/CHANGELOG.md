# Changelog

## [1.2.0](https://github.com/0xHoaxen/shogun/compare/services/shinobi/v1.1.0...services/shinobi/v1.2.0) (2026-10-20)


### Features

* **kagami:** scan each owner's follow-ups on their own clock ([fe868aa](https://github.com/0xHoaxen/shogun/commit/fe868aa0208b0be1d73a7a5289a11e196c1bf1ae))
* **llm:** call each user's own provider and drop the shared Anthropic settings ([7d1ef65](https://github.com/0xHoaxen/shogun/commit/7d1ef65cb0319da08d4040b2aced3aa9fe413863))
* multi-user phase 11 (sign-up, per-user providers, account deletion) ([1517322](https://github.com/0xHoaxen/shogun/commit/1517322d2b8015be13d7dd5ff37e0a8b73a0d6d5))
* **root:** guard sqlc queries with an owner-scope check ([930c495](https://github.com/0xHoaxen/shogun/commit/930c495772179f1d41ef0d5f72ee9958aedd8b62))
* **shinobi:** purge a deleted user's sources, postings and scores ([6fe4c4e](https://github.com/0xHoaxen/shogun/commit/6fe4c4e17dc8d2cc95ec7dfe8ac366d8bee8c871))
* **shinobi:** read each owner's source schedules in their own time zone ([91a4c48](https://github.com/0xHoaxen/shogun/commit/91a4c48b4c482e2225beaaeea828e96759eb4a01))
* **torii:** announce users to services and keep a local owners list ([10ffeb1](https://github.com/0xHoaxen/shogun/commit/10ffeb1a2fd0671448dbf2fbec7b559073f481dc))

## [1.1.0](https://github.com/0xHoaxen/shogun/compare/services/shinobi/v1.0.0...services/shinobi/v1.1.0) (2026-10-15)


### Features

* **shinobi:** delete a discovery source ([e5407bd](https://github.com/0xHoaxen/shogun/commit/e5407bd71030c254cf50bdd9c00585ea05f64475))
* **shinobi:** delete a discovery source ([ac6101d](https://github.com/0xHoaxen/shogun/commit/ac6101d08d9d34f92e1740711c603a81c95474bf))
* **shinobi:** say when a source's address is a web page ([41640cf](https://github.com/0xHoaxen/shogun/commit/41640cf2e59fcd2194610cf9fe8048ce3fbafd37))
* **shinobi:** say when a source's address is a web page ([820af84](https://github.com/0xHoaxen/shogun/commit/820af84f925ba0fc649730bfaf16749953a94dda))


### Bug Fixes

* **web:** draw select chevron inside the field padding ([7d6333a](https://github.com/0xHoaxen/shogun/commit/7d6333aa89a88d52556ccc0190a107495b8d5197))

## [1.0.0](https://github.com/0xHoaxen/shogun/compare/services/shinobi/v0.0.1...services/shinobi/v1.0.0) (2026-10-10)


### Features

* **bus:** add consumer management and relay functionality ([a9e02b5](https://github.com/0xHoaxen/shogun/commit/a9e02b50b054262aaa526f003934d282b16cf797))
* **bus:** add consumer management and relay functionality ([32bcfaf](https://github.com/0xHoaxen/shogun/commit/32bcfaf1fc4c58552334ee6a0c34b07e227700dc))
* **deploy:** add migrate subcommand and enable migration Job ([12f9f8d](https://github.com/0xHoaxen/shogun/commit/12f9f8d5a1f5f24ae0a4d6507ded47f5aa493939))
* **deploy:** manifests and tooling for a cluster deploy (P10.5a-d) ([ea1878a](https://github.com/0xHoaxen/shogun/commit/ea1878a1e55256c89572598ec80a3841ca80aa94))
* phase 10 hardening and operations ([297b946](https://github.com/0xHoaxen/shogun/commit/297b9468a391faeb219e1a256ab6c789960618c8))
* phase 9 (dojo, katana, shinobi, sensei) ([f6dc0f3](https://github.com/0xHoaxen/shogun/commit/f6dc0f3f540bbe6f65a5f722fb9f4b0f667f548a))
* **repo:** generate the ten service skeletons ([25b1e55](https://github.com/0xHoaxen/shogun/commit/25b1e554d8c371bff3266870d65e0dbde6f2d40b))
* service skeletons ([c626699](https://github.com/0xHoaxen/shogun/commit/c6266991fc34d4c73b838ea046f5bd778711f0d0))
* **shinobi:** discovery tables, protos, domain rules and store ([c69b2a1](https://github.com/0xHoaxen/shogun/commit/c69b2a157b87d8139c8f3acad6c21a96140bd07c))
* **shinobi:** read sources on a schedule and on demand ([5bd95c7](https://github.com/0xHoaxen/shogun/commit/5bd95c710f9d3da0d48891c24dbc4bf208ae1ed1))
* **shinobi:** save a posting to the tracker ([2f4dff6](https://github.com/0xHoaxen/shogun/commit/2f4dff6a32662cc80bd1dce64c28ce42690b3d1f))
* **shinobi:** score postings and announce matches ([35cb2b1](https://github.com/0xHoaxen/shogun/commit/35cb2b13fd45c38c198e9726a2f28028c704be75))


### Bug Fixes

* add the go.sum entries that go work sync left out ([34966ea](https://github.com/0xHoaxen/shogun/commit/34966eaaa32395f5aaf6bae624a4203d5d95c1c1))

## Changelog
