# SocialFlow.ai launch film

A 32-second, 1920x1080, 60 fps launch film made in code. `film.js` draws a simplified version of the SocialFlow.ai
interface in the Dither theme, using the Lucide icon set, the integration marks, the feature artwork and the
Converge logo, so every part of it can move. Its sound is made in code with
[earshot](https://github.com/Leonxlnx/earshot).

```sh
devenv shell -- bun run render:launch-film
```

The command writes `tmp/launch-film/openpost-launch.mp4`. Each step can be rerun alone:

| Step               | Command                                                                                          | Output in `tmp/launch-film/`                |
| ------------------ | ------------------------------------------------------------------------------------------------ | ------------------------------------------- |
| Render the picture | `bun apps/marketing/launch-film/render.mjs`                                                      | `picture.mp4`, `cues.json`                  |
| Score the music    | `uv run apps/marketing/launch-film/music.py tmp/launch-film/cues.json tmp/launch-film/music.wav` | `music.wav`                                 |
| Mix the sound      | `uv run apps/marketing/launch-film/mix.py tmp/launch-film`                                       | `kit/`, `soundtrack.wav`, `mix-report.json` |

To render stills while you edit, run `bun apps/marketing/launch-film/render.mjs --stills 4.2,11,28.6`.

## The cut

The film is one continuous move through the product. Each scene hands an element to the next:

1. The headline types, and the composer draws itself around it.
2. On the downbeat, the composer bursts into five native network cards. The X card gets its own shorter version, and
   the launch image drops into every card.
3. The camera zooms through the X card's image into the image editor, where the image is finished.
4. The canvas shrinks into the video monitor while clips, captions and music fill the timeline.
5. The video lands in Friday 09:00. The week fills up, then a publishing sweep turns each post from scheduled to
   published, including one failure that retries.
6. Results and replies follow, then a feature roll on the beat, then the logo.

## How it fits together

- `film.js` owns the timeline. Every frame is a pure function of time. The renderer calls `window.film.seek(t)` and
  captures the stage, so playback speed never depends on the machine.
- Layout positions sit on a fixed grid in `left` and `top`. Motion is a transform on top of them, so alignment does not
  drift during animation.
- Scene boundaries sit on bar lines at 120 BPM. `film.js` also emits the sound cues (`cues.json`), so picture and
  sound share one source of truth.
- `script.json` holds the post, its shorter X version and the reply.
- Character limits come from `@openpost/platform-text`: X 280, Bluesky 300, Threads and Mastodon 500, LinkedIn 3,000.
- `music.py` builds the bed from the film's sections: an A minor progression, earshot's physically modelled mallets,
  oscillator pads and bass, and synthesized drums. It steps down 12 dB before the two big hits.
- `mix.py` renders `kit.json` (earshot's `launch` preset, with the hits tuned to A) and places every take by its
  measured onset or peak. It sets each effect's level against the music bed and masters the mix to -14 LUFS with
  true peak at or below -1.2 dBTP.

## Provenance

- The interface is an illustration of the app, not a capture. Accounts, analytics and replies are sample data, and the
  results scenes are labelled as such.
- Integration marks come from `assets/postiz-socials/`, the set the README uses. Feature artwork comes from
  `assets/brand/features/`.
- The music and effects contain no recordings. earshot's bank sounds are made in code, and its kit preset notes where
  a sound was fitted from a CC0 recording.

## Checking the sound

You can't judge the soundtrack by reading code. Measure it with earshot after a change:

```sh
E='uvx --python 3.12 --from earshot@git+https://github.com/Leonxlnx/earshot@591fa3e028032e6dce56620a20e932e35667e8d8 earshot'
$E critique tmp/launch-film/openpost-launch.mp4
$E sheet tmp/launch-film/openpost-launch.mp4 -o tmp/launch-film/sheet.png --cues tmp/launch-film/cues.json
```

On 24 October 2026 the film scored launch-grade: 22 of 23 metrics fell inside the range of earshot's seven reference
launch films, with sound on 88% of visual events. The only open item is realness, rated uncertain (p_real 0.62),
because the music is synthesized. To get a produced sound, replace `music.wav` with a licensed 120 BPM track that keeps
the bar structure, then run `mix.py` again.
