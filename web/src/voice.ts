// Voice engine: microphone, one RTCPeerConnection to the relay pod that owns
// the call, per-user playback and level meters. No React in here.
//
// Offer recipe (agreed with media-relay): 1 sendrecv audio m-line (mic) +
// N-1 recvonly ones ("slots"), plus DataChannel "meta" where the relay says
// which slot (mid) carries which user. N >= 10 so a direct call that turns into
// a group never needs renegotiation.
import { api, ApiError } from './api.ts'
import { rms, Speaking } from './speaking.ts'

export type AudioPrefs = { inputId?: string; outputId?: string; echoCancel: boolean; noiseSuppress: boolean }

export type Stats = {
  rttMs?: number
  jitterMs?: number
  lossPct?: number
  sendKbps?: number
  remote?: string // relay ICE address actually used
}

export type VoiceHooks = {
  onConnection: (state: RTCPeerConnectionState | 'offering') => void
  onSpeaking: (speaking: Set<string>) => void // userIds (me included)
  onLevel: (level: number) => void // my mic, 0..1, ~20 Hz
  onStats: (s: Stats) => void
  onLog: (type: string, data?: unknown) => void
  onFatal: (reason: string) => void // call closed / not a participant: stop retrying
  onBlockedAutoplay: () => void
}

const MIN_SLOTS = 10
const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

type Remote = { el: HTMLAudioElement; analyser: AnalyserNode; buf: Float32Array<ArrayBuffer> }

export class Voice {
  private callId: string
  private me: string
  private capacity: number
  private prefs: AudioPrefs
  private hooks: VoiceHooks

  private mic: MediaStream | null = null
  private ctx: AudioContext | null = null
  private micAnalyser: AnalyserNode | null = null
  private pc: RTCPeerConnection | null = null
  private sender: RTCRtpSender | null = null
  private remotes = new Map<string, Remote>() // by mid
  private slots: Record<string, string> = {} // mid -> userId
  private detectors = new Map<string, Speaking>()
  private speaking = new Set<string>()
  private timers: ReturnType<typeof setInterval>[] = []
  private generation = 0 // bumps on every (re)connect; stale async work checks it
  private stopped = false

  muted = false
  deafened = false
  ptt = false
  talking = false // PTT key held

  constructor(callId: string, me: string, capacity: number, prefs: AudioPrefs, hooks: VoiceHooks) {
    this.callId = callId
    this.me = me
    this.capacity = capacity
    this.prefs = prefs
    this.hooks = hooks
  }

  async start() {
    this.ctx = new AudioContext()
    await this.openMic()
    this.timers.push(setInterval(() => this.levels(), 50), setInterval(() => this.stats(), 1000))
    await this.connect('start')
  }

  stop() {
    this.stopped = true
    this.generation++
    this.timers.forEach(clearInterval)
    this.pc?.close()
    this.remotes.forEach((r) => r.el.remove())
    this.mic?.getTracks().forEach((t) => t.stop())
    this.ctx?.close()
  }

  /** New PeerConnection + offer, e.g. after relay_moved or a failed connection. */
  async connect(reason: string) {
    const gen = ++this.generation
    this.pc?.close()
    this.remotes.forEach((r) => r.el.remove())
    this.remotes.clear()
    this.slots = {}
    this.hooks.onConnection('offering')
    this.hooks.onLog('rtc_offer', { reason })

    const pc = new RTCPeerConnection({ iceServers: [] })
    this.pc = pc
    const track = this.mic!.getAudioTracks()[0]
    this.sender = pc.addTransceiver(track, { direction: 'sendrecv', streams: [this.mic!] }).sender
    for (let i = 0; i < Math.max(this.capacity, MIN_SLOTS) - 2; i++) pc.addTransceiver('audio', { direction: 'recvonly' })
    const meta = pc.createDataChannel('meta')
    meta.onmessage = (m) => {
      this.slots = JSON.parse(m.data).slots ?? {}
      this.hooks.onLog('rtc_slots', this.slots)
    }
    pc.ontrack = (e) => this.addRemote(e.transceiver.mid ?? '', e.track)
    pc.onconnectionstatechange = () => {
      if (gen !== this.generation) return
      this.hooks.onConnection(pc.connectionState)
      this.hooks.onLog('rtc_state', pc.connectionState)
      if (pc.connectionState === 'failed') this.connect('connection failed') // relay pod restarted or moved
    }

    await pc.setLocalDescription(await pc.createOffer())
    await iceGathered(pc)
    for (let wait = 1000; ; wait = Math.min(wait * 2, 8000)) {
      if (gen !== this.generation) return
      try {
        const { sdp } = await api<{ sdp: string }>(`/calls/${this.callId}/webrtc`, {
          method: 'POST', body: { sdp: pc.localDescription!.sdp },
        })
        if (gen !== this.generation) return
        await pc.setRemoteDescription({ type: 'answer', sdp })
        this.applyTrack()
        return
      } catch (err) {
        if (err instanceof ApiError && err.status === 409 && !/retry/.test(err.message)) {
          this.hooks.onFatal(err.message) // call not active / not a participant
          return
        }
        this.hooks.onLog('rtc_retry', { error: (err as Error).message, wait })
        await sleep(wait)
      }
    }
  }

  setMuted(on: boolean) { this.muted = on; this.applyTrack() }
  setPtt(on: boolean) { this.ptt = on; this.applyTrack() }
  setTalking(on: boolean) { this.talking = on; this.applyTrack() }
  setDeafened(on: boolean) {
    this.deafened = on
    this.remotes.forEach((r) => (r.el.muted = on))
    this.applyTrack()
  }

  /** Mic is live only when not muted, not deafened and (no PTT or key held).
   *  Disabled tracks still send silent Opus: the relay counts packets, not voice. */
  private applyTrack() {
    const live = !this.muted && !this.deafened && (!this.ptt || this.talking)
    this.mic?.getAudioTracks().forEach((t) => (t.enabled = live))
  }

  async setPrefs(prefs: AudioPrefs) {
    const micChanged = prefs.inputId !== this.prefs.inputId || prefs.echoCancel !== this.prefs.echoCancel ||
      prefs.noiseSuppress !== this.prefs.noiseSuppress
    this.prefs = prefs
    if (micChanged && this.mic) {
      const old = this.mic
      await this.openMic()
      await this.sender?.replaceTrack(this.mic!.getAudioTracks()[0]) // no renegotiation
      old.getTracks().forEach((t) => t.stop())
      this.applyTrack()
    }
    this.remotes.forEach((r) => this.sink(r.el))
  }

  resumeAudio() {
    this.ctx?.resume()
    this.remotes.forEach((r) => r.el.play().catch(() => {}))
  }

  private async openMic() {
    this.mic = await navigator.mediaDevices.getUserMedia({
      audio: {
        deviceId: this.prefs.inputId ? { exact: this.prefs.inputId } : undefined,
        echoCancellation: this.prefs.echoCancel,
        noiseSuppression: this.prefs.noiseSuppress,
        autoGainControl: true,
      },
    })
    this.micAnalyser = this.analyser(this.mic)
  }

  private analyser(stream: MediaStream): AnalyserNode {
    const a = this.ctx!.createAnalyser()
    a.fftSize = 1024
    this.ctx!.createMediaStreamSource(stream).connect(a)
    return a
  }

  private addRemote(mid: string, track: MediaStreamTrack) {
    const stream = new MediaStream([track])
    const el = new Audio()
    el.srcObject = stream // Chrome only feeds WebAudio from a remote track that is also playing
    el.muted = this.deafened
    this.sink(el)
    el.play().catch(() => this.hooks.onBlockedAutoplay())
    const analyser = this.analyser(stream)
    this.remotes.set(mid, { el, analyser, buf: new Float32Array(analyser.fftSize) })
  }

  private sink(el: HTMLAudioElement) {
    if (this.prefs.outputId && 'setSinkId' in el) el.setSinkId(this.prefs.outputId).catch(() => {})
  }

  private levels() {
    const now = performance.now()
    const next = new Set<string>()
    const check = (user: string, level: number) => {
      let d = this.detectors.get(user)
      if (!d) this.detectors.set(user, (d = new Speaking()))
      if (d.update(level, now)) next.add(user)
    }
    if (this.micAnalyser) {
      const buf = new Float32Array(this.micAnalyser.fftSize)
      this.micAnalyser.getFloatTimeDomainData(buf)
      const level = rms(buf)
      this.hooks.onLevel(Math.min(1, level * 4))
      const sending = this.mic?.getAudioTracks()[0]?.enabled
      if (sending) check(this.me, level)
    }
    for (const [mid, r] of this.remotes) {
      const user = this.slots[mid]
      if (!user) continue
      r.analyser.getFloatTimeDomainData(r.buf)
      check(user, rms(r.buf))
    }
    if (next.size !== this.speaking.size || [...next].some((u) => !this.speaking.has(u))) {
      this.speaking = next
      this.hooks.onSpeaking(next)
    }
  }

  private async stats() {
    if (!this.pc || this.stopped) return
    const report = await this.pc.getStats()
    const s: Stats = {}
    let lost = 0, received = 0, jitter = 0, n = 0
    report.forEach((r) => {
      if (r.type === 'candidate-pair' && r.nominated && r.state === 'succeeded') {
        if (r.currentRoundTripTime != null) s.rttMs = Math.round(r.currentRoundTripTime * 1000)
        const remote = report.get(r.remoteCandidateId)
        const addr = remote?.address ?? remote?.ip // hidden by Chrome for peer-reflexive candidates
        if (addr) s.remote = `${addr}:${remote.port}`
      }
      if (r.type === 'inbound-rtp' && r.kind === 'audio' && r.packetsReceived > 0) {
        lost += r.packetsLost ?? 0
        received += r.packetsReceived
        jitter += r.jitter ?? 0
        n++
      }
      if (r.type === 'outbound-rtp' && r.kind === 'audio') {
        const prev = this.lastSent
        this.lastSent = { bytes: r.bytesSent, at: r.timestamp }
        if (prev) s.sendKbps = Math.round(((r.bytesSent - prev.bytes) * 8) / (r.timestamp - prev.at))
      }
    })
    if (n) {
      s.jitterMs = Math.round((jitter / n) * 1000)
      s.lossPct = Math.round((lost / Math.max(1, lost + received)) * 1000) / 10
    }
    this.hooks.onStats(s)
  }
  private lastSent: { bytes: number; at: number } | null = null
}

/** No trickle ICE: the offer goes out with every candidate (host only, instant). */
function iceGathered(pc: RTCPeerConnection): Promise<void> {
  if (pc.iceGatheringState === 'complete') return Promise.resolve()
  return new Promise((resolve) => {
    const done = () => {
      if (pc.iceGatheringState === 'complete') {
        pc.removeEventListener('icegatheringstatechange', done)
        resolve()
      }
    }
    pc.addEventListener('icegatheringstatechange', done)
    setTimeout(resolve, 2000) // never block forever on a slow interface
  })
}
