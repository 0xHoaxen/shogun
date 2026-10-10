# Changelog

## [0.2.0](https://github.com/0xHoaxen/shogun/compare/services/kagami/v0.1.0...services/kagami/v0.2.0) (2026-10-10)


### Features

* **bus:** add consumer management and relay functionality ([a9e02b5](https://github.com/0xHoaxen/shogun/commit/a9e02b50b054262aaa526f003934d282b16cf797))
* **bus:** add consumer management and relay functionality ([32bcfaf](https://github.com/0xHoaxen/shogun/commit/32bcfaf1fc4c58552334ee6a0c34b07e227700dc))
* **deploy:** add migrate subcommand and enable migration Job ([12f9f8d](https://github.com/0xHoaxen/shogun/commit/12f9f8d5a1f5f24ae0a4d6507ded47f5aa493939))
* **deploy:** manifests and tooling for a cluster deploy (P10.5a-d) ([ea1878a](https://github.com/0xHoaxen/shogun/commit/ea1878a1e55256c89572598ec80a3841ca80aa94))
* **fude:** drafts, hanko approval and tsubame mail (P7) ([7d4ad95](https://github.com/0xHoaxen/shogun/commit/7d4ad95d9e72d6c118755925a3ed5bd131a65722))
* **kagami:** add companies, jobs, contacts and imports tables ([71807fd](https://github.com/0xHoaxen/shogun/commit/71807fda33ebb63ea95c41499aea98d77378c40f))
* **kagami:** add contact RPCs ([77f22f2](https://github.com/0xHoaxen/shogun/commit/77f22f2f8cf2a16ac3d9845a8d318780bebd2164))
* **kagami:** add follow-up scans and ListDueFollowUps ([8bc8672](https://github.com/0xHoaxen/shogun/commit/8bc86726ff38ac36cbbd59c27df9c121d2f68dcc))
* **kagami:** add job and contact state machines ([f6f6f81](https://github.com/0xHoaxen/shogun/commit/f6f6f814e76b8bf3123a18b2f2669826e05cac76))
* **kagami:** add job RPCs with outbox events ([c030c48](https://github.com/0xHoaxen/shogun/commit/c030c48d5e6652c2b26bc5756206356a4dbd1616))
* **kagami:** add sqlc store with versioned updates and pagination ([ccb410d](https://github.com/0xHoaxen/shogun/commit/ccb410d9a5dac0a441772b6e0e74dc27b5fca8ff))
* **kagami:** apply mail and sent-draft events to jobs and contacts ([4a9fc0c](https://github.com/0xHoaxen/shogun/commit/4a9fc0c9bad6f7fef90bffd5848364287dd8847f))
* **kagami:** find the contact and job a piece of mail is about ([340c244](https://github.com/0xHoaxen/shogun/commit/340c244c211ba4af5d77fad09bed8da0c3e80d12))
* **kagami:** import contacts from CSV ([316e8c0](https://github.com/0xHoaxen/shogun/commit/316e8c0f52e7b3772603145693ee9ce637e7d913))
* **kagami:** name the owner in follow-up and status events ([12e8ee1](https://github.com/0xHoaxen/shogun/commit/12e8ee11dfc1c0dbc1722127c1fe97ffee4ce9d4))
* **kagami:** name the owner in job.added and contact.status_changed ([14a58bc](https://github.com/0xHoaxen/shogun/commit/14a58bc2fdb2dd7f6f9344ab5237e08566e024e2))
* **kagami:** return company names with jobs and contacts ([fd69c93](https://github.com/0xHoaxen/shogun/commit/fd69c93feffd73c23e0b6a09effc3d2c093d3bf3))
* phase 10 hardening and operations ([297b946](https://github.com/0xHoaxen/shogun/commit/297b9468a391faeb219e1a256ab6c789960618c8))
* phase 9 (dojo, katana, shinobi, sensei) ([f6dc0f3](https://github.com/0xHoaxen/shogun/commit/f6dc0f3f540bbe6f65a5f722fb9f4b0f667f548a))
* phrase 4 ([c59ef02](https://github.com/0xHoaxen/shogun/commit/c59ef02dcb2608ba09198b20b407235ba89b9774))
* **pkg:** let services run their own jobs on the relay's River client ([293ccae](https://github.com/0xHoaxen/shogun/commit/293ccae261f761d19bb14319f9bc410dd0547bb3))
* **repo:** generate the ten service skeletons ([25b1e55](https://github.com/0xHoaxen/shogun/commit/25b1e554d8c371bff3266870d65e0dbde6f2d40b))
* **sensei:** project events into facts ([671fde7](https://github.com/0xHoaxen/shogun/commit/671fde70dba52da7ccba65e02b7e479d4234cdf7))
* service skeletons ([c626699](https://github.com/0xHoaxen/shogun/commit/c6266991fc34d4c73b838ea046f5bd778711f0d0))
* **taiko:** notification settings and a shared daily schedule ([a3ff8c5](https://github.com/0xHoaxen/shogun/commit/a3ff8c542d91be542445a29ec68083961c6d850b))
* **taiko:** notifications, live stream and daily digest (Phase 8) ([49f755e](https://github.com/0xHoaxen/shogun/commit/49f755e9871e5575981054caefe17890ed754d63))


### Bug Fixes

* add the go.sum entries that go work sync left out ([34966ea](https://github.com/0xHoaxen/shogun/commit/34966eaaa32395f5aaf6bae624a4203d5d95c1c1))

## 1.0.0 (2026-10-02)


### Features

* **bus:** add consumer management and relay functionality ([a9e02b5](https://github.com/0xHoaxen/shogun/commit/a9e02b50b054262aaa526f003934d282b16cf797))
* **bus:** add consumer management and relay functionality ([32bcfaf](https://github.com/0xHoaxen/shogun/commit/32bcfaf1fc4c58552334ee6a0c34b07e227700dc))
* **deploy:** add migrate subcommand and enable migration Job ([12f9f8d](https://github.com/0xHoaxen/shogun/commit/12f9f8d5a1f5f24ae0a4d6507ded47f5aa493939))
* **repo:** generate the ten service skeletons ([25b1e55](https://github.com/0xHoaxen/shogun/commit/25b1e554d8c371bff3266870d65e0dbde6f2d40b))
* service skeletons ([c626699](https://github.com/0xHoaxen/shogun/commit/c6266991fc34d4c73b838ea046f5bd778711f0d0))
