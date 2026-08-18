import { useState } from "react";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Loader2, Search } from "lucide-react";

interface VideoInputProps {
  onSubmit: (url: string) => void;
  isLoading?: boolean;
}

// Mirrors services.ParseVideoID on the backend so the user gets instant
// feedback instead of a round trip.
//
// The previous check was `url.includes("youtube.com") || url.includes("tiktok.com")`,
// which rejected youtu.be — the form YouTube's own share button produces, and
// therefore the most common way anyone would paste a link — while accepting
// TikTok URLs the backend has never supported.
const YOUTUBE_PATTERNS = [
  /^[A-Za-z0-9_-]{11}$/,
  /(?:youtube\.com|youtube-nocookie\.com)\/watch\?(?:.*&)?v=[A-Za-z0-9_-]{11}/,
  /youtu\.be\/[A-Za-z0-9_-]{11}/,
  /(?:youtube\.com|youtube-nocookie\.com)\/(?:shorts|embed|live|v)\/[A-Za-z0-9_-]{11}/,
];

export const isLikelyYouTubeUrl = (url: string): boolean =>
  YOUTUBE_PATTERNS.some((p) => p.test(url.trim()));

const EXAMPLES = [
  { label: "Carbonara", id: "dQw4w9WgXcQ" },
  { label: "Thai green curry", id: "9bZkp7q19f0" },
  { label: "Smash burgers", id: "kJQP7kiw5Fk" },
];

export const VideoInput = ({ onSubmit, isLoading = false }: VideoInputProps) => {
  const [url, setUrl] = useState("");
  const [touched, setTouched] = useState(false);

  const trimmed = url.trim();
  const invalid = touched && trimmed !== "" && !isLikelyYouTubeUrl(trimmed);

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (!trimmed || !isLikelyYouTubeUrl(trimmed)) return;
    onSubmit(trimmed);
  };

  const applyExample = (id: string) => {
    const exampleUrl = `https://youtu.be/${id}`;
    setUrl(exampleUrl);
    setTouched(true);
    onSubmit(exampleUrl);
  };

  return (
    <div className="w-full max-w-2xl mx-auto space-y-3">
      <form onSubmit={handleSubmit} className="flex flex-col sm:flex-row gap-3">
        <div className="flex-1">
          <Input
            type="text"
            inputMode="url"
            placeholder="Paste a YouTube recipe video link…"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            onBlur={() => setTouched(true)}
            aria-label="YouTube video URL"
            aria-invalid={invalid}
            className={`w-full h-12 text-base ${invalid ? "border-destructive focus-visible:ring-destructive" : ""}`}
          />
          {invalid && (
            <p className="text-sm text-destructive mt-1.5">
              That does not look like a YouTube link. Try a youtube.com/watch, youtu.be, or /shorts URL.
            </p>
          )}
        </div>
        <Button type="submit" disabled={isLoading} className="h-12 px-6 shrink-0">
          {isLoading ? (
            <>
              <Loader2 className="mr-2 h-4 w-4 animate-spin" />
              Working…
            </>
          ) : (
            <>
              <Search className="mr-2 h-4 w-4" />
              Find ingredients
            </>
          )}
        </Button>
      </form>

      <div className="flex flex-wrap items-center gap-2 text-sm">
        <span className="text-muted-foreground">Try one:</span>
        {EXAMPLES.map((ex) => (
          <button
            key={ex.id}
            type="button"
            onClick={() => applyExample(ex.id)}
            disabled={isLoading}
            className="px-3 py-1 rounded-full border border-border text-muted-foreground hover:text-foreground hover:border-foreground/30 transition-colors disabled:opacity-50"
          >
            {ex.label}
          </button>
        ))}
      </div>
    </div>
  );
};
