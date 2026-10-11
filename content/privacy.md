+++
title = "Privacy"
description = "What Fazon Nexus stores about you, who can see it, where it lives, and how to download or delete it."
+++

*Last updated: 11 October 2026.*

Fazon Nexus is a personal site run by one person (Faizan). You can read everything on it without
an account. An account lets you comment, like comments, log your immersion on Track and keep your
Kyōtsu trainer history. This page explains, in plain English, what that involves.

## What's stored

Only what you give the site or what it needs to work:

- **Account:** your email address, your display name, when you joined, and your role (user,
  moderator or admin). If a moderator or admin has blocked you from commenting, when that happened
  and who did it.
- **Profile:** the optional "About me" text and profile picture. Pictures are re-encoded on the
  server as a small square JPEG, so the original file and its metadata (such as camera or location
  data) are not kept.
- **Comments and likes:** the comments you post (with the page and time), and which comments you
  liked.
- **Notifications:** replies to your comments and likes on them (admins are also notified of new
  comments).
- **Track:** the hours you log per category and sub-category, your sub-category names, Kotu and
  Kyōtsu scores, weekly summaries (and whether each one is private), your weekly self-ratings and
  goals, and whether your Track is public.
- **Kyōtsu trainer sessions:** when you trained, the settings you used and your answers.
- **API keys:** if you create any, their name, permission (read or read & write), a short prefix
  and when they were created and last used. The keys themselves are stored only as a one-way
  fingerprint (hash), so nobody can read them back.
- **Logins:** your login sessions and one-time login and sign-up links, all stored only as hashes.
  A sign-up that hasn't been confirmed yet keeps just the email address, name and the page to go
  back to, until its link expires an hour later.

There are no passwords, no analytics, no ads and no tracking scripts. Like most servers, the API
keeps a technical log (for example sign-in and sign-up requests with the IP address and email
address, and moderation actions) for troubleshooting and to prevent abuse.

## Why

To run your account (sending login and sign-up emails, keeping you logged in), and to provide the
features you use: comments, likes, notifications, profiles, Track, the Kyōtsu trainer and API keys.

## What's public and what's private

**Anyone can see**, without logging in:

- your display name, profile picture, "About me", when you joined, your level and points (the
  likes your comments received), and how many comments you've posted;
- your comments, on the pages where you posted them and the most recent ones on your profile, and
  how many likes each comment has (not who liked it);
- your **Track**, unless you turn it off: hours per category and sub-category, Kotu and Kyōtsu
  scores (Kyōtsu scores include the results of your trainer sessions), self-ratings and weekly
  summaries that you haven't marked private. These show on the leaderboard, the summaries feed and
  your profile **once the week has ended** (or when the admin ends it early, in its last two
  hours), never while it's still running. Your goals stay private until then too; after that, if
  you met at least one of them, the goal achievers board shows your goals and hours for that
  week. With your Track turned off, none of it is shown to anyone else.

**Only you can see:** your email address, your notifications, the comments you liked, your private
summaries, your Kyōtsu session history, your API keys, and your current week on Track.

**Moderators and admins** can see a list of accounts with display names, roles, when people joined
and were last active, how many comments they've posted, whether they're blocked from commenting
and whether their Track is public. **Only admins** can see email addresses there. Moderators can
delete comments and block people from commenting; admins can also change roles and delete
accounts.

## Where it's stored and who processes it

- **The website** (the pages you read) is hosted on **GitHub Pages**. Like any web host, GitHub
  receives your IP address when you load a page.
- **The API and database** (your account and everything listed above) run on a server rented from
  **Hetzner** in **Helsinki, Finland**.
- **Login and sign-up emails** are sent through **Resend**, which receives your email address and
  the email's contents (the one-time link).

## Cookies and browser storage

There's **one cookie**, `fz_session`, set only when you log in. It keeps you logged in: it's
`HttpOnly` (scripts can't read it), sent only over HTTPS, and expires 90 days after you last used
the site (each visit extends it, at most once a day). Logging out deletes it.

The site also saves a few preferences in your browser's local storage. They never leave your
device:

- `theme`: light or dark mode;
- `fz-track-acc-view`: which view of the Track accuracy graph you picked;
- `fz-track-sw:…` and `fz-track-swq:…`: the Track stopwatch, so a running timer survives a
  reload;
- `kyotsu-layout` and `kyotsu-pause`: Kyōtsu trainer settings;
- `kyotsu-test-speaker`, `kyotsu-test` and `kyotsu-test-…`: the Kyōtsu test page's speaker and
  the items you flagged;
- `nendai-birthyear`: the birth year you entered in the Nendai tool.

## How long it's kept

Your data is kept while you have an account. Expired login and sign-up links are cleared every
hour, and sessions end when you log out or 90 days after you last used the site.

When an account is deleted, it and its data are removed from the live database **immediately**.
The server keeps **daily backups for 14 days**, so deleted data disappears from the backups within
14 days.

## Your choices and rights

- **See and download your data:** on your profile, under **Your data**, "Download my data" gives
  you everything listed above as one JSON file.
- **Correct it:** you can change your display name, "About me", picture and Track data yourself at
  any time, and hide your Track from others.
- **Delete it:** on your profile, under **Your data**, you can delete your account. Everything goes
  at once; comments that other people replied to stay as "[deleted]", with no name or text, so the
  replies still make sense.
- **Ask a question or make a request:** {{< contact >}}.
