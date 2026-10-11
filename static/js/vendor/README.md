# Vendored third-party scripts

Used only by `js/fazon-md.js` (Markdown for Track weekly summaries), which loads them on demand on
the Track pages and the profile page. Files are byte-for-byte the official builds; they match the
npm registry tarballs (`dist.integrity` checked). Do not edit them; to upgrade, download the new
version, verify it the same way, update the file names in `js/fazon-md.js` and this table.

| File | Package | Source URL | SHA-256 |
|---|---|---|---|
| `marked-18.0.6.umd.js` | marked 18.0.6 (MIT) | https://cdn.jsdelivr.net/npm/marked@18.0.6/lib/marked.umd.js | `62ad5de5bea6d79b4c47e5c0b5cbe4be61e25ee8994595c2cc0969b2a144cc5d` |
| `purify-3.4.12.min.js` | DOMPurify 3.4.12 (Apache-2.0 OR MPL-2.0) | https://cdn.jsdelivr.net/npm/dompurify@3.4.12/dist/purify.min.js | `c45ba939765574f96cbf35ee9b6d89f73756a17921814425e74b82f7c54603ce` |
| `marked-18.0.6.LICENSE.txt` | marked licence | https://cdn.jsdelivr.net/npm/marked@18.0.6/LICENSE | `8e3a3f82f59a60958f56ca08f445647c32a4733dc7ca6c2c46f6eb898471ab9c` |
| `dompurify-3.4.12.LICENSE.txt` | DOMPurify licence | https://cdn.jsdelivr.net/npm/dompurify@3.4.12/LICENSE | `cfc7749b96f63bd31c3c42b5c471bf756814053e847c10f3eb003417bc523d30` |

npm tarball integrity: `marked-18.0.6.tgz` sha512-MrV5puXBfuiy6wl6DLaq3BtIJQAJToAd5zt/ZKhRfGRAuFPALE7/4Y7jnxRQoEgK/pBgurGqLyAuRgZ2xOjr6w==,
`dompurify-3.4.12.tgz` sha512-zQvGet8Z2sWbQhCmfFz/T5QWH2oBmjnqK3qvOjaqaNLrLEF912WamU+ohnTp0TCep/MFVHpdJuCZEdFOdTnEFg==.

Check: `sha256sum static/js/vendor/*.js`. (The trailing `sourceMappingURL` comments point at maps
that are not vendored; browsers only request them with dev tools open.)
