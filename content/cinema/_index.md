+++
title = "Cinema Planner"
description = "Upcoming cinema listings across the North of England, plus a planner that fits the films you want to see into the days you choose."
layout = "cinema"
# Where the page fetches the listings (CORS-enabled JSON). For local testing the page also accepts
# ?api=http://localhost:PORT/… or ?api=http://127.0.0.1:PORT/… (no other hosts).
api = "https://cinema-api.fazon.nexus/v1/listings.json"
# HTML only: no RSS feed for this section; kept out of the sitemap and of page lists.
outputs = ["html"]
# Hidden from search engines (site head.html emits noindex, nofollow).
private = true
[sitemap]
  disable = true
[build]
  list = "never"
+++
