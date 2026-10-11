+++
title = "Admin"
# /admin/ — users, roles and moderation for moderators and admins (layouts/admin/section.html,
# js/fazon-admin.js). Not in the main menu: the account menu links it for moderators and admins.
# Everyone else sees "Not allowed"; the API enforces every rule server-side anyway.
private = true
outputs = ["html"]
[build]
  list = "never"
[sitemap]
  disable = true
+++
