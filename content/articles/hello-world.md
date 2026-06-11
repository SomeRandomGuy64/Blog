+++
title = "Hello, World"
date = 2026-06-11
draft = true
tags = ["meta"]
categories = ["updates"]
+++

This is a placeholder article so the listing page has something to show. Delete it
whenever you like, or create a new one with:

```bash
hugo new content articles/my-post.md
```

## Furigana

Use the `ruby` shortcode for furigana — write `{{</* ruby 漢字 かんじ */>}}` and it renders as
{{< ruby 漢字 かんじ >}}. The reading is wrapped in `lang="ja"` so it picks up correct
Japanese rendering.

## Embedding WebGL

Drop the same WebGL canvas from the home page into any article with the `webgl`
shortcode:

{{< webgl >}}

That's the foundation for the data-vis tools later — each article can carry its own
canvas.
