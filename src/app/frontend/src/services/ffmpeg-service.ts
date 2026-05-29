import { FFmpeg } from "@ffmpeg/ffmpeg";
import { fetchFile } from "@ffmpeg/util";
import classWorkerURL from "./ffmpeg-worker.js?worker&url";
import coreURL from "@ffmpeg/core?url";
import wasmURL from "@ffmpeg/core/wasm?url";
import {
  decimalSecondsToNanoseconds,
  hmsToNanoseconds,
} from "@/utils/tams-time";

export interface ProbeResult {
  hasVideo: boolean;
  hasAudio: boolean;
  duration: number;
  durationNanoseconds?: bigint;
  startTimeNanoseconds?: bigint;
  videoCodec?: string;
  audioCodec?: string;
  width?: number;
  height?: number;
  frameRate?: { numerator: number; denominator: number };
  sampleRate?: number;
  channels?: number;
  keyFrameCount?: number;
}

function gcd(a: number, b: number): number {
  while (b !== 0) {
    const next = a % b;
    a = b;
    b = next;
  }
  return Math.abs(a);
}

function decimalToRatio(
  value: string,
): { numerator: number; denominator: number } | undefined {
  const parsed = Number(value);
  if (!Number.isFinite(parsed) || parsed <= 0) return undefined;

  const [, fraction = ""] = value.split(".");
  const denominator = fraction ? 10 ** fraction.length : 1;
  const numerator = Math.round(parsed * denominator);
  const divisor = gcd(numerator, denominator);
  return {
    numerator: numerator / divisor,
    denominator: denominator / divisor,
  };
}

class FFmpegService {
  private ffmpeg: FFmpeg | null = null;
  private loaded = false;
  private loading: Promise<void> | null = null;

  async load(): Promise<void> {
    if (this.loaded) return;
    if (this.loading) return this.loading;

    this.loading = (async () => {
      this.ffmpeg = new FFmpeg();
      await this.ffmpeg.load({ classWorkerURL, coreURL, wasmURL });
      this.loaded = true;
    })();

    return this.loading;
  }

  async probe(
    file: File,
    options: { countKeyFrames?: boolean } = {},
  ): Promise<ProbeResult> {
    await this.load();
    const ff = this.ffmpeg!;

    await ff.writeFile("input", await fetchFile(file));

    const logs: string[] = [];
    const logHandler = ({ message }: { message: string }) => {
      logs.push(message);
    };
    ff.on("log", logHandler);

    try {
      // Capture probe metadata from FFmpeg log output without writing media.
      await ff.exec(["-i", "input", "-f", "null", "-"]).catch(() => {});
    } finally {
      ff.off("log", logHandler);
    }

    const output = logs.join("\n");

    let hasVideo = false;
    let hasAudio = false;
    let videoCodec: string | undefined;
    let audioCodec: string | undefined;
    let width: number | undefined;
    let height: number | undefined;
    let frameRate: { numerator: number; denominator: number } | undefined;
    let sampleRate: number | undefined;
    let channels: number | undefined;
    let duration = 0;
    let durationNanoseconds: bigint | undefined;
    let startTimeNanoseconds: bigint | undefined;
    let keyFrameCount: number | undefined;

    // Parse duration: "Duration: 00:01:30.50"
    const durMatch = output.match(/Duration:\s*(\d+):(\d+):(\d+\.?\d*)/);
    if (durMatch) {
      durationNanoseconds = hmsToNanoseconds(
        durMatch[1],
        durMatch[2],
        durMatch[3],
      );
      duration = Number(durationNanoseconds) / 1_000_000_000;
    }

    const startMatch = output.match(/start:\s*(-?\d+(?:\.\d+)?)/);
    if (startMatch) {
      const sign = startMatch[1].startsWith("-") ? -1n : 1n;
      const decimal = startMatch[1].replace(/^-/, "");
      startTimeNanoseconds = sign * decimalSecondsToNanoseconds(decimal);
    }

    // Parse video stream: "Stream #0:N...: Video: h264 ..., 1920x1080, 25 fps"
    const videoLine = output.match(
      /Stream\s+#\d+[:.]\d+.*:\s*Video:[^\n]+/,
    )?.[0];
    const videoMatch = videoLine?.match(/Video:\s*(\w+).*?,\s*(\d+)x(\d+)/);
    if (videoMatch) {
      hasVideo = true;
      videoCodec = videoMatch[1];
      width = parseInt(videoMatch[2], 10);
      height = parseInt(videoMatch[3], 10);
      const fpsMatch = videoLine?.match(/,\s*([0-9]+(?:\.[0-9]+)?)\s*fps\b/);
      frameRate = fpsMatch ? decimalToRatio(fpsMatch[1]) : undefined;
    }

    // Parse audio stream: "Stream #0:N...: Audio: aac, 48000 Hz, stereo"
    const audioMatch = output.match(
      /Stream\s+#\d+[:.]\d+.*:\s*Audio:\s*(\w+).*?,\s*(\d+)\s*Hz(?:.*?,\s*(\w+))?/,
    );
    if (audioMatch) {
      hasAudio = true;
      audioCodec = audioMatch[1];
      sampleRate = parseInt(audioMatch[2], 10);
      // Parse channel layout to count
      const layout = audioMatch[3];
      if (layout === "mono") channels = 1;
      else if (layout === "stereo") channels = 2;
      else if (layout === "5.1" || layout === "5.1(side)") channels = 6;
      else channels = 2; // default
    }

    if (options.countKeyFrames && hasVideo) {
      const frameLogs: string[] = [];
      const frameLogHandler = ({ message }: { message: string }) => {
        frameLogs.push(message);
      };
      ff.on("log", frameLogHandler);
      try {
        await ff
          .exec(["-i", "input", "-vf", "showinfo", "-f", "null", "-"])
          .catch(() => {});
      } finally {
        ff.off("log", frameLogHandler);
      }
      keyFrameCount = frameLogs.join("\n").match(/iskey:\s*1/g)?.length ?? 0;
    }

    await ff.deleteFile("input");

    return {
      hasVideo,
      hasAudio,
      duration,
      durationNanoseconds,
      startTimeNanoseconds,
      videoCodec,
      audioCodec,
      width,
      height,
      frameRate,
      sampleRate,
      channels,
      keyFrameCount,
    };
  }

  async segment(
    file: File,
    segDuration: number,
    mode: "video" | "audio" | "both",
  ): Promise<Blob[]> {
    await this.load();
    const ff = this.ffmpeg!;

    await ff.writeFile("input", await fetchFile(file));

    const args: string[] = ["-i", "input"];

    if (mode === "video") {
      args.push("-an"); // strip audio
    } else if (mode === "audio") {
      args.push("-vn"); // strip video
    }

    args.push(
      "-c",
      "copy",
      "-f",
      "segment",
      "-segment_time",
      String(segDuration),
      "-reset_timestamps",
      "1",
      "-segment_format",
      "mpegts",
      "out%03d.ts",
    );

    await ff.exec(args);

    // Collect output segments
    const blobs: Blob[] = [];
    for (let i = 0; ; i++) {
      const name = `out${String(i).padStart(3, "0")}.ts`;
      try {
        const data = await ff.readFile(name);
        const bytes =
          data instanceof Uint8Array
            ? new Uint8Array(data)
            : new TextEncoder().encode(data as string);
        blobs.push(new Blob([bytes], { type: "video/mp2t" }));
        await ff.deleteFile(name);
      } catch {
        break; // no more segments
      }
    }

    await ff.deleteFile("input");

    return blobs;
  }
}

export const ffmpegService = new FFmpegService();
