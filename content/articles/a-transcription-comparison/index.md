+++
title = 'A Transcription Comparison'
date = 2026-06-11T13:33:33+01:00
draft = false
tags = ["transcription", "AI", "OCR"]
categories = ["transcription", "AI", "OCR"]
+++

On my quest to find new content for my Japanese immersion I came across this video here: [【完全解説】よくわかる『スクウェア・エニックス』栄光と低迷の40年史 1982～2026【保存版】](https://www.youtube.com/watch?v=kSdY-lq0oRM)

It's a video about the history of Square Enix. I thought "Cool, let's get this video transcribed", but then I noticed that the video itself has baked-in subtitles.

{{< figure src="screenshot-1.png" alt="Screenshot of the video with baked-in Japanese subtitles" caption="*Dragon Quest and Final Fantasy*" >}}

I usually transcribe my videos with Soniox, which takes the audio and writes a transcription using it, however this time there a perfectly good subtitles I could use, I just needed a way to get them.

My first thought was to use OCR (Optical Character Recognition) to get the text, I've used it in English in the past and it tends to work fine but I was worried that it wouldn't pick up the kanji very well. So I had an idea.

## The Experiment

To determine what was best I decided to transcribe the video twice, using OCR and Soniox. For OCR I used EasyOCR and had Claude handle it for me before giving me a comparison. 

Here are the results Claude gave me:

{{< figure src="screenshot-2.png" alt="Screenshot of the Claude comparison, showing that Soniox beats EasyOCR" caption="*As we can see, Soniox wins*" >}}

So there we have it, you should always transcribe with audio right? Wait a second there, not everyone has the money to spend on Soniox and EasyOCR is free, so shouldn't we compare EasyOCR to an audio transcription model that's also free?

So I went ahead and did just that. I used Whisper v3 large and ran it on my machine locally to compare against EasyOCR's transcription.

Here are the results:

{{< figure src="screenshot-3.png" alt="Screenshot of the Claude comparison with Whisper, Soniox and EasyOCR, showing that both Whisper and Soniox beats EasyOCR" caption="*Yet again, EasyOCR loses*" >}}

And here we see EasyOCR losing again, it can't keep up with either the Soniox or Whisper models. Now, one caveat is that running a local model does require a good GPU, if you don't have that or the money for Soniox then it may be worth using EasyOCR, but at that point I'd probably recommend sticking to content with human made transcripts available.

I'd personally ignore Claude's comparison between Whisper and Soniox as Claude doesn't know how accurate either are to the original video. Also ignore anything it says about ElevenLabs, it is more expensive than Soniox but I don't know where its getting the 17x cheaper figure from or why it even included ElevenLabs in the first place. In my experience Soniox is roughlt around 4x - 6x cheaper than ElevenLabs (which is still significant).

## Conclusion

OCR just isn't there yet for Japanese. It's a shame considering these videos have subtitles, presumably written by a human, which would have been perfect, but alas, we can't use them for our immersion learning. I'd just recommend using Soniox or a local model, it's frustrating but there's nothing much we can do about it.