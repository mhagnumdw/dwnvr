// dwnvr's MSE player.
//
// The recorded segments are standalone fMP4s that always start at t=0, so
// stitching them into a continuous timeline is just positioning each one with
// `timestampOffset`. Nothing is remuxed or decoded outside the browser - the
// server only delivers bytes.
//
// The time reference is the wall clock: `base` is the real instant that
// corresponds to currentTime=0. That way "jump to 14:32" becomes arithmetic.

import { mediaURL, messageOf, media } from './api.ts';
import type { Segment } from './api.ts';

// How much to keep buffered ahead and behind. Ahead covers Wi-Fi latency;
// behind allows going back a few seconds without refetching everything. The one
// ahead is the base at 1×: above that, #lookahead() rules.
const AHEAD_S = 25;
const BEHIND_S = 15;

// How long a wait must last to become "carregando…" on screen. At 16× on a
// phone, playback alternates waiting/playing several times a second - each
// micro-wait for bytes or for decoding - and the notice blinked nonstop, more
// annoying than the wait it announces. The desktop never blinked because it
// never gets to wait. Disappearing is still immediate: holding the notice after
// the video came back would be a lie.
const NOTICE_MS = 400;

// How to get past a corrupted stretch. A packet lost on the network ruins the
// frames until the next keyframe, and the player does not know where it is - so
// it advances step by step until it finds a point that decodes. The step is
// fixed and short on purpose: doubling it got there in fewer tries, but
// overshot the good point (measured on 11/09/2026: it landed on 15:27:12 when
// 15:27:10 already played). Past the ceiling, it gives up on the segment and
// goes to the start of the next one, which always opens on a keyframe.
const JUMP_STEP_MS = 1000;
const JUMP_MAX_MS = 10000;

// The jump of the double tap on the sides of the video and of the keyboard
// arrows, as on YouTube. Each extra tap or key adds another.
export const FAST_JUMP_MS = 10000;
// How far a forward jump goes before the end of what exists. Landing exactly on
// the end would make seek() go back to the start of the last segment.
const FAST_JUMP_SLACK_MS = 1000;

export class Player {
  video: HTMLVideoElement | null = null;

  base = $state(0);
  currentMs = $state(0);
  playing = $state(false);
  buffering = $state(false);
  // From the start of a seek until the window ahead is in the buffer, without
  // the `buffering` delay: it is how the thumbnail strip knows when to get out
  // of the way.
  loading = $state(false);
  error = $state('');
  rate = $state(1);

  #cam = '';
  #gens: string[] = [];
  #segments: Segment[] = [];
  #ms: MediaSource | null = null;
  #sb: SourceBuffer | null = null;
  #next = 0;
  #initAppended: string | null = null;
  #pumping: Promise<void> | null = null; // promise of the in-flight pump, or null when there is none
  #generation = 0; // invalidates in-flight work after a seek
  // Cancels the downloads of the generation that was just invalidated. Without
  // this, the old pump held up the new one until the segment nobody wants
  // anymore finished arriving - and also shared bandwidth with it.
  #abort = new AbortController();
  #seeking = false; // seek in flight: `base` and `currentTime` are out of sync
  #noticeTimer: number | undefined; // counts the ms of waiting before lighting up "carregando…"

  #mime: string | null = null; // which codecs the current SourceBuffer was created with
  // eslint-disable-next-line svelte/prefer-svelte-reactivity -- internal cache, not on screen
  #mimes = new Map<string, string>(); // generation -> mime, to avoid refetching the same init
  #boundary: number | null = null; // start of the segment where the tracks change and it has to restart
  #jump: { start: number; target: number } | null = null; // last jump over corrupted data, in ms
  // Whether whoever jumped had the video paused, while that jump's seek is in
  // flight. The <video>'s `paused` does not work here: #reset swaps the src,
  // and the video stays paused until the seek ends, whether or not it was
  // playing before.
  #skipPaused: boolean | null = null;

  attach(video: HTMLVideoElement) {
    this.video = video;
    video.addEventListener('timeupdate', this.#onTime);
    // 'seeked' matters as much as 'timeupdate': when jumping with the video
    // paused, no timeupdate happens and the clock would stay stuck on --:--:--
    // even with the frame already on screen.
    video.addEventListener('seeked', this.#onTime);
    video.addEventListener('loadeddata', this.#onTime);
    video.addEventListener('waiting', this.#onWaiting);
    video.addEventListener('playing', this.#onPlaying);
    video.addEventListener('pause', this.#onPause);
    video.addEventListener('ratechange', this.#onRate);
    video.addEventListener('error', this.#onError);
  }

  destroy() {
    const v = this.video;
    if (!v) return;
    v.removeEventListener('timeupdate', this.#onTime);
    v.removeEventListener('seeked', this.#onTime);
    v.removeEventListener('loadeddata', this.#onTime);
    v.removeEventListener('waiting', this.#onWaiting);
    v.removeEventListener('playing', this.#onPlaying);
    v.removeEventListener('pause', this.#onPause);
    v.removeEventListener('ratechange', this.#onRate);
    v.removeEventListener('error', this.#onError);
    // Leaving the screen drops the segment that was arriving: nobody sees it
    // anymore.
    this.#newGeneration();
    // A scheduled notice that fires after this would light up "carregando…" for
    // a player that no longer exists - and nobody would clear it.
    this.#setBuffering(false);
    // The same goes for `playing`: the listeners are gone, so the stop just
    // below no longer fires 'pause' and the flag would stay stuck at true - and
    // whoever draws a button from it would show ⏸ for a dead video.
    this.playing = false;
    // Releasing the MediaSource and revoking the URL matters: without it the
    // decoded video buffer stays pinned until the garbage collector comes by,
    // which on a phone means tens of MB.
    try {
      if (this.#ms?.readyState === 'open') this.#ms.endOfStream();
    } catch {
      // the MediaSource was already closing: nothing to end
    }
    if (v.src.startsWith('blob:')) URL.revokeObjectURL(v.src);
    v.removeAttribute('src');
    v.load();
    this.video = null;
    this.#ms = null;
    this.#sb = null;
  }

  setSource(cam: string, gens: string[], segments: Segment[]) {
    this.#cam = cam;
    this.#gens = gens;
    this.#segments = segments;
    this.#mimes.clear();
    this.#boundary = null;
    this.#jump = null;
    this.#newGeneration();
    // The new generation above prevents the `finally` of an in-flight seek from
    // running, so the gate has to be opened here: a new day with no recording
    // at all does not call seek(), and the clock would stay mute forever.
    this.#seeking = false;
    this.loading = false;
    this.base = 0;
    this.currentMs = 0;
  }

  // Updates the segment list without interrupting what is playing.
  //
  // Today grows while the screen is open. Redoing setSource() + seek() on every
  // update would recreate the MediaSource and send the video back to the
  // starting point; here only the list changes, and the pump continues from
  // where it stopped - which also makes playback unstick by itself on reaching
  // the live edge.
  async updateSegments(gens: string[], segments: Segment[]) {
    // Swapping the arrays in the middle of a pump would make it advance the
    // index over the new list. Waiting is cheaper than invalidating the
    // generation, which would also abort an in-flight seek; a pump lasts the
    // download of a few segments.
    await this.#pumping;

    // Where the pump would resume, in wall-clock time. Retention may delete the
    // start of the day and shift all the indexes, so the position in the old
    // list is not a valid reference.
    const pending = this.#segments[this.#next];
    const last = this.#segments.at(-1);
    const resumeMs = pending ? pending[0] : last ? last[0] + 1 : 0;

    this.#gens = gens;
    this.#segments = segments;
    const i = segments.findIndex(([start]) => start >= resumeMs);
    this.#next = i < 0 ? segments.length : i;

    await this.#pump();
  }

  #newGeneration() {
    this.#abort.abort();
    this.#abort = new AbortController();
    return ++this.#generation;
  }

  get hasSegments() {
    return this.#segments.length > 0;
  }

  get firstMs() {
    return this.#segments[0]?.[0] ?? 0;
  }

  get lastMs() {
    const s = this.#segments.at(-1);
    return s ? s[0] + s[1] : 0;
  }

  // Only path to `buffering`: lighting up is deferred, clearing is immediate.
  #setBuffering(on: boolean) {
    clearTimeout(this.#noticeTimer);
    this.#noticeTimer = undefined;
    if (!on) {
      this.buffering = false;
      return;
    }
    this.#noticeTimer = setTimeout(() => {
      this.buffering = true;
    }, NOTICE_MS);
  }

  #onTime = () => {
    if (!this.video) return;
    // During a seek, `base` is already the target segment's while `currentTime`
    // is still the one that was playing - or 0, right after the src swap.
    // Publishing that would throw the timeline marker to the start of the
    // segment while the network delivers the bytes, and only then would it land
    // where you clicked. What closes the gate is the seek itself, which already
    // put `currentMs` on the target. The pump keeps running: it is what fills
    // the buffer.
    if (!this.#seeking) this.currentMs = this.base + this.video.currentTime * 1000;
    void this.#pump();
  };

  #onWaiting = () => {
    this.#setBuffering(true);
    if (this.#crossBoundary()) return;
    this.#skipGap();
  };

  // Reached the end of what could be buffered before a track change: playback
  // keeps going by restarting the MediaSource from there. It costs a short
  // rebuffer, at a point that only exists when someone turns a camera's audio
  // on or off - in exchange, the whole recording stays navigable.
  #crossBoundary() {
    if (this.#boundary === null) return false;
    // Only crosses when there is really nothing more ahead: a `waiting` in the
    // middle of the buffer is a bad network, not a boundary.
    const b = this.#sb?.buffered;
    if (b?.length && this.video && b.end(b.length - 1) - this.video.currentTime > 0.5) return false;

    const ms = this.#boundary;
    this.#boundary = null;
    void this.seek(ms);
    return true;
  }

  #onPlaying = () => {
    this.#setBuffering(false);
    this.playing = true;
  };

  #onPause = () => {
    this.playing = false;
  };

  #onRate = () => {
    this.rate = this.video?.playbackRate ?? 1;
  };

  // Corrupted data in the recording - a packet lost over UDP between the camera
  // and go2rtc - takes down the browser's decoder for good: the <video> goes
  // into error and no append gets through anymore. ffmpeg hides the same damage
  // and carries on; Chrome gives up. Without this, playback stopped there until
  // someone clicked further ahead on the timeline.
  //
  // Only the decode error: the others are not solved by jumping - an init the
  // browser rejects would be rejected again, and the loop would never end.
  #onError = () => {
    if (this.video?.error?.code !== MediaError.MEDIA_ERR_DECODE) return;
    if (!this.hasSegments) return;

    // Failed again right after the last jump: it is the same damaged stretch,
    // and the ceiling counts from where it started. Far from there it is a new
    // stretch.
    const where = this.currentMs;
    const last = this.#jump;
    const start = last && where < last.target + JUMP_STEP_MS ? last.start : where;

    let target = where + JUMP_STEP_MS;
    if (target - start > JUMP_MAX_MS) {
      const following = this.#segments[this.#indexAt(where) + 1];
      if (!following) return;
      target = following[0];
    }
    // Past the end of what exists there is no good point to go to. Asking
    // seek() for that would send it back to the start of the last segment,
    // straight into the same damaged stretch.
    if (target >= this.lastMs) return;

    this.#jump = { start, target };
    void this.seek(target);
  };

  /** Index of the last segment that starts at ms or before. */
  #indexAt(ms: number) {
    let lo = 0;
    let hi = this.#segments.length - 1;
    let found = -1;
    while (lo <= hi) {
      const mid = (lo + hi) >> 1;
      const s = this.#segments[mid];
      if (s && s[0] <= ms) {
        found = mid;
        lo = mid + 1;
      } else {
        hi = mid - 1;
      }
    }
    return found;
  }

  async seek(ms: number) {
    const video = this.video;
    let i = Math.max(this.#indexAt(ms), 0);
    const found = this.#segments[i];
    if (!found || !video) return;

    // If the target fell in a gap, start from the next segment that exists.
    const [start, dur] = found;
    if (ms > start + dur && i + 1 < this.#segments.length) i++;
    const [targetMs, targetDur, targetGen] = this.#segments[i] ?? found;

    const gen = this.#newGeneration();
    this.#seeking = true;
    this.#skipPaused = null;
    this.loading = true;
    this.error = '';
    this.#setBuffering(true);
    // A new seek overrides any boundary that was scheduled.
    this.#boundary = null;

    // Where playback will actually land: inside the segment, or at its start
    // when the click fell before what exists or after what ended.
    const inside = (ms - targetMs) / 1000;
    const isInside = inside > 0 && inside < targetDur / 1000;
    // The marker goes there already in this frame. Swapping the src zeroes
    // `currentTime` and the buffer only arrives after a few requests: without
    // this the cursor would sit at the start of the segment the whole time.
    this.currentMs = isInside ? ms : targetMs;

    try {
      // The generation is the TARGET SEGMENT'S, not the first of the day:
      // turning audio on in the middle of the day opens a new generation, and
      // creating the SourceBuffer with the old one's codecs made every
      // appendBuffer fail.
      await this.#reset(video, targetMs, this.#genDe(targetGen), gen);
      if (gen !== this.#generation) return; // another seek got there first
      this.#next = i;
      await this.#repump();
      if (gen !== this.#generation) return;

      if (isInside) video.currentTime = inside;
      // Reflects the position immediately: autoplay may be blocked by the
      // browser, and in that case no playback event would come to update the
      // clock or the timeline marker.
      this.currentMs = this.base + video.currentTime * 1000;
      await video.play().catch(() => {
        // autoplay blocked: the clock was already set above, and the ▶ stays
      });
    } catch (e) {
      // The error of a run-over seek belongs to the camera or stretch from
      // before.
      if (gen === this.#generation) this.error = messageOf(e);
    } finally {
      // A run-over seek does not give back the gate or clear "carregando…":
      // that state now belongs to the new seek, which is still fetching bytes.
      if (gen === this.#generation) {
        this.#seeking = false;
        this.#setBuffering(false);
      }
    }

    if (gen === this.#generation) void this.#fillThenRelease(gen);
  }

  // Moves `deltaMs` from where the video is, backward or forward.
  //
  // seek() recreates the MediaSource and refetches everything, which on a 10 s
  // jump would light up "carregando…" on every tap. But the target is almost
  // always already in the buffer - BEHIND_S behind, the #lookahead() window
  // ahead -, and then it is enough to change `currentTime`. seek() is left for
  // when it is not: right after another seek, too far, in a recording gap or
  // beyond a track boundary, which never reaches the buffer.
  //
  // Consecutive jumps add up: during a seek, `currentMs` is already its target
  // (the #seeking gate holds it there), and outside a seek `currentTime`
  // changes immediately. In both cases the next jump starts from where the
  // previous one sent it.
  //
  // Paused stays paused, as on YouTube - and seek() ends in play().
  jump(deltaMs: number) {
    const video = this.video;
    if (!video || !this.base || !this.hasSegments) return;

    const from = this.#seeking ? this.currentMs : this.base + video.currentTime * 1000;
    let target = Math.max(from + deltaMs, this.firstMs);
    if (deltaMs > 0) target = Math.min(target, Math.max(from, this.lastMs - FAST_JUMP_SLACK_MS));

    const t = (target - this.base) / 1000;
    const b = this.#sb?.buffered;
    let inBuffer = false;
    if (b && !this.#seeking) {
      for (let i = 0; i < b.length; i++) {
        if (t >= b.start(i) && t < b.end(i)) inBuffer = true;
      }
    }
    if (inBuffer) {
      video.currentTime = t;
      // `seeked` also updates, but only after the frame arrives: this way the
      // clock and the marker already go there now.
      this.currentMs = target;
      return;
    }

    // During a seek, the intent is that of the jump that started it; one from
    // another origin - a click on the timeline - ends in play().
    const paused = this.#seeking ? (this.#skipPaused ?? false) : video.paused;
    const seek = this.seek(target);
    // After seek(), which clears it when it starts.
    this.#skipPaused = paused;
    // seek() swaps the generation before the first await. Another seek that
    // arrives later - a click on the timeline - rules over play and is not
    // paused here.
    const gen = this.#generation;
    void seek.then(() => {
      if (gen !== this.#generation) return;
      this.#skipPaused = null;
      if (paused) video.pause();
    });
  }

  // The seek's pump measures the window ahead from the 0 of the new
  // MediaSource, and the video only jumps into the segment afterwards: a click
  // near its end leaves a few seconds in the buffer. The strip only gets out of
  // the way when the window is full from where the video is. It stays outside
  // the seek's await, because whoever waits on it - the pause of `paused=1`,
  // the detection sheet - wants the first frame, not the full window.
  async #fillThenRelease(gen: number) {
    if (!this.error) await this.#repump();
    if (gen === this.#generation) this.loading = false;
  }

  // The init generation of a segment, by the index the timeline sends.
  #genDe(index: number) {
    const g = this.#gens[index];
    if (g === undefined) throw new Error('segmento sem init na timeline');
    return g;
  }

  // `seek` is the number of the seek that asked for the swap.
  async #reset(video: HTMLVideoElement, baseMs: number, gen: string, seek: number) {
    this.base = baseMs;
    this.#next = 0;
    this.#initAppended = null;
    // The old buffer stops receiving right now, and not only when the src
    // swaps: the old video keeps playing while the init arrives, and a pump
    // fired by its timeupdate would now read the NEW list from index 0 -
    // downloading the first segment of the day into the wrong buffer, or
    // marking the track boundary at the start of the day.
    this.#sb = null;

    const mime = await this.#mimeFor(gen, this.#abort.signal);
    // Another seek started while the init was coming - in a quick camera
    // switch, the new camera's, with an init that may have arrived first. The
    // <video> is now its own: swapping the src here would tear down what it
    // already built, and the player would be left stuck on an empty
    // MediaSource.
    if (seek !== this.#generation) return;
    if (!MediaSource.isTypeSupported(mime)) {
      throw new Error(`este navegador não reproduz ${mime}`);
    }

    // Swapping the src zeroes playbackRate. Before, that only happened on a
    // seek, where nobody notices; now crossing a track boundary restarts the
    // MediaSource on its own, and losing the 8× in the middle of a review would
    // be a shock.
    const rate = video.playbackRate;

    if (video.src.startsWith('blob:')) URL.revokeObjectURL(video.src);
    const ms = new MediaSource();
    this.#ms = ms;
    video.src = URL.createObjectURL(ms);
    await new Promise<void>((r) => {
      ms.addEventListener(
        'sourceopen',
        () => {
          r();
        },
        { once: true },
      );
    });
    // Same check as before, now for the sourceopen wait: the SourceBuffer of a
    // MediaSource that already left the <video> would take the place of the new
    // seek's.
    if (seek !== this.#generation || this.#ms !== ms) return;
    video.playbackRate = rate;

    const sb = ms.addSourceBuffer(mime);
    sb.mode = 'segments';
    this.#sb = sb;
    this.#mime = mime;
  }

  // The codecs come from the real init, not from a guess based on the camera
  // name: the same installation mixes H265 with audio, H265 without audio and
  // H264.
  async #mimeFor(gen: string, signal: AbortSignal) {
    const cached = this.#mimes.get(gen);
    if (cached) return cached;

    const buf = new Uint8Array(await media(mediaURL.init(this.#cam, gen), signal));
    const at = (o: number) => String.fromCharCode(...buf.subarray(o, o + 4));
    const map: Readonly<Record<string, string>> = {
      hev1: 'hev1.1.6.L153.B0',
      hvc1: 'hvc1.1.6.L153.B0',
      avc1: 'avc1.640029',
      fLaC: 'flac',
      Opus: 'opus',
      mp4a: 'mp4a.40.2',
    };
    // eslint-disable-next-line svelte/prefer-svelte-reactivity -- local Set, only for collecting
    const codecs = new Set<string>();
    for (let i = 0; i + 8 <= buf.length; i++) {
      const c = map[at(i + 4)];
      if (c) codecs.add(c);
    }
    const mime = `video/mp4; codecs="${[...codecs].join(',')}"`;
    this.#mimes.set(gen, mime);
    return mime;
  }

  // How much to request ahead now. The window is measured in seconds of
  // recording and spent in seconds of wall clock: at 16× the 25s ahead last
  // 1.5s of playback, and the video would spend all its time waiting for bytes.
  // Scaling by the rate gives back the same breathing room. The ceiling exists
  // because the window is also memory - 16× of it would be minutes of decoded
  // video, which on a phone is what kills the tab. It is a method, and not a
  // variable of the pump, so that changing the speed in the middle of a pump
  // applies on the very next round.
  #lookahead() {
    return AHEAD_S * Math.min(Math.max(this.video?.playbackRate ?? 1, 1), 4);
  }

  #bufferedEnd() {
    const b = this.#sb?.buffered;
    return b?.length ? b.end(b.length - 1) : (this.video?.currentTime ?? 0);
  }

  // Keeps a sliding window ahead of playback. Appending a whole day would be
  // gigabytes; the window is what makes the timeline navigable on modest
  // hardware.
  //
  // One pump at a time: whoever arrives in the middle of another gets the
  // promise of the one already running, instead of polling a flag to know when
  // it ended. Dropping the call is on purpose - the in-flight pump fills the
  // same window, and queueing one per timeupdate would make the queue grow
  // without end on a slow network.
  #pump(): Promise<void> {
    if (this.#pumping) return this.#pumping;
    if (!this.#sb || this.#ms?.readyState !== 'open') return Promise.resolve();

    const p = this.#pumpOnce().finally(() => {
      if (this.#pumping === p) this.#pumping = null;
    });
    this.#pumping = p;
    return p;
  }

  // Waits for the in-flight pump to let go and runs another. It is what seek()
  // needs: it just changed the resume point, and the pump that started before
  // that is no good - just receiving its promise would leave the video with
  // nothing attached. The wait is short: the new generation canceled the old
  // pump's download.
  async #repump() {
    await this.#pumping;
    return this.#pump();
  }

  async #pumpOnce() {
    const gen = this.#generation;
    const signal = this.#abort.signal;

    try {
      while (
        gen === this.#generation &&
        this.#next < this.#segments.length &&
        this.video &&
        this.#bufferedEnd() - this.video.currentTime < this.#lookahead()
      ) {
        const seg = this.#segments[this.#next];
        if (!seg) break;
        const [start, , gi] = seg;
        const g = this.#genDe(gi);

        // The init only needs to be reattached when the generation changes.
        if (this.#initAppended !== g) {
          // But the new generation may change the TRACKS, not just the SPS:
          // turning a camera's audio on in the middle of the day adds an audio
          // track. A SourceBuffer does not accept that even with changeType -
          // Chrome rejects the whole append with "Got unexpected audio track".
          // The only way out is to restart the MediaSource, and that is what
          // #boundary schedules.
          //
          // When only the SPS changed (resolution, for example) the mime stays
          // the same and reattaching the init is enough: it is the common case
          // and goes on with no seam.
          const mime = await this.#mimeFor(g, signal);
          // The mime of an invalidated generation says nothing about the
          // current buffer: comparing it would mark the boundary on another
          // seek's player.
          if (gen !== this.#generation) break;
          if (mime !== this.#mime) {
            this.#boundary = start;
            break;
          }
          await this.#append(mediaURL.init(this.#cam, g), null, signal);
          if (gen !== this.#generation) break;
          this.#initAppended = g;
        }
        await this.#append(mediaURL.segment(this.#cam, start), (start - this.base) / 1000, signal);
        // A seek during the download already pointed `#next` at its own
        // segment; advancing here would make the new pump skip exactly that
        // one.
        if (gen !== this.#generation) break;
        this.#next++;
      }
      if (gen === this.#generation) await this.#evict();
    } catch (e) {
      if (gen === this.#generation) this.error = messageOf(e);
    }
  }

  async #append(url: string, offset: number | null, signal: AbortSignal) {
    // The SourceBuffer is the one from when the download started. A seek in the
    // middle of it - the user's or #onError's - swaps the MediaSource, and
    // bytes arriving afterwards would land in the new buffer, before its init
    // and with the old timestampOffset.
    const ms = this.#ms;
    const sb = this.#sb;
    const buf = await media(url, signal);
    if (!sb || this.#sb !== sb || ms?.readyState !== 'open') return;
    if (offset !== null) sb.timestampOffset = offset;
    await new Promise<void>((res, rej) => {
      sb.addEventListener(
        'updateend',
        () => {
          res();
        },
        { once: true },
      );
      sb.addEventListener(
        'error',
        () => {
          rej(new Error('falha ao anexar segmento'));
        },
        {
          once: true,
        },
      );
      sb.appendBuffer(buf);
    });
  }

  async #evict() {
    const sb = this.#sb;
    if (!sb || !this.video) return;
    const keepFrom = this.video.currentTime - BEHIND_S;
    if (keepFrom <= 0 || !sb.buffered.length) return;
    const start = sb.buffered.start(0);
    if (keepFrom - start < BEHIND_S) return;
    await new Promise<void>((res) => {
      sb.addEventListener(
        'updateend',
        () => {
          res();
        },
        { once: true },
      );
      sb.remove(start, keepFrom);
    });
  }

  // Gaps in the recording stall the video at the end of the buffered range;
  // without a nudge it stays stuck there forever.
  #skipGap() {
    const b = this.#sb?.buffered;
    if (!b || !this.video) return;
    for (let i = 0; i < b.length; i++) {
      if (b.start(i) > this.video.currentTime) {
        this.video.currentTime = b.start(i);
        return;
      }
    }
  }

  toggle() {
    if (!this.video) return;
    if (this.video.paused) {
      if (!this.base && this.hasSegments) void this.seek(this.firstMs);
      else
        this.video.play().catch(() => {
          // the browser refused to play: the button keeps showing ▶
        });
    } else {
      this.video.pause();
    }
  }

  // Pause without toggling. toggle() serves the button, which always knows what
  // state it is in; whoever comes from the URL with `paused=1` needs to order a
  // stop, and order it AFTER the first seek landed - because seek() ends in
  // play().
  pause() {
    this.video?.pause();
  }

  setRate(r: number) {
    if (this.video) this.video.playbackRate = r;
  }
}
