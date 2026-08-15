import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { AlertTriangle, FlaskConical, Loader2, MapPin, Youtube, Zap } from "lucide-react";

import { VideoInput } from "@/components/VideoInput";
import { IngredientsList } from "@/components/IngredientsList";
import { StoresList } from "@/components/StoresList";
import { ApiError, processVideoUrl, type RecipeResponse } from "@/services/api";
import { toast } from "sonner";

/** DemoBanner keeps a fixture response from ever passing as a live one. */
const DemoBanner = ({ notice }: { notice: string }) => (
  <div className="flex gap-3 items-start rounded-lg border border-amber-300 bg-amber-50 dark:border-amber-800 dark:bg-amber-950/40 px-4 py-3">
    <FlaskConical className="h-5 w-5 text-amber-600 dark:text-amber-500 shrink-0 mt-0.5" />
    <div className="text-sm">
      <p className="font-medium text-amber-900 dark:text-amber-200">Demo mode</p>
      <p className="text-amber-800 dark:text-amber-300/90">{notice}</p>
    </div>
  </div>
);

/** Thumbnail falls back to a placeholder tile rather than hiding itself.
 *  Simply hiding a failed image left a large empty gap in the card, which reads
 *  as a broken layout instead of a missing picture. */
const Thumbnail = ({ src }: { src: string }) => {
  const [failed, setFailed] = useState(false);

  if (!src || failed) {
    return (
      <div className="w-full sm:w-64 aspect-video shrink-0 rounded-lg bg-muted grid place-items-center text-muted-foreground">
        <Youtube className="h-8 w-8 opacity-40" aria-hidden="true" />
      </div>
    );
  }
  return (
    <img
      src={src}
      alt=""
      className="w-full sm:w-64 aspect-video shrink-0 object-cover rounded-lg bg-muted"
      loading="lazy"
      onError={() => setFailed(true)}
    />
  );
};

const ErrorBanner = ({ error }: { error: ApiError }) => {
  const hint: Record<string, string> = {
    network: "Start the backend with `go run ./cmd` in market-mate-be, then try again.",
    url: "Copy the link straight from YouTube's share button.",
    video: "Check the video is public and the link is complete.",
    ingredients: "Try a video whose description actually lists the recipe.",
    stores: "The ingredients above are still valid.",
  };
  return (
    <div className="flex gap-3 items-start rounded-lg border border-destructive/40 bg-destructive/5 px-4 py-3">
      <AlertTriangle className="h-5 w-5 text-destructive shrink-0 mt-0.5" />
      <div className="text-sm">
        <p className="font-medium text-destructive">{error.message}</p>
        {hint[error.stage] && <p className="text-muted-foreground mt-0.5">{hint[error.stage]}</p>}
      </div>
    </div>
  );
};

const Index = () => {
  const [data, setData] = useState<RecipeResponse | null>(null);

  const mutation = useMutation<RecipeResponse, ApiError, string>({
    mutationFn: processVideoUrl,
    onSuccess: (result) => {
      setData(result);
      toast.success(
        result.cached
          ? "Loaded from cache"
          : `Found ${result.ingredients.length} ingredients and ${result.stores.length} stores`,
      );
    },
    onError: (err) => toast.error(err.message),
  });

  const handleSubmit = (url: string) => {
    setData(null);
    mutation.mutate(url);
  };

  return (
    <div className="min-h-screen bg-background">
      <div className="max-w-5xl mx-auto px-6 py-12 space-y-10">
        <header className="text-center space-y-3">
          <h1 className="text-4xl sm:text-5xl font-bold tracking-tight">MarketMate</h1>
          <p className="text-lg text-muted-foreground max-w-xl mx-auto">
            Paste a cooking video. Get the shopping list, and somewhere nearby to buy it.
          </p>
        </header>

        <VideoInput onSubmit={handleSubmit} isLoading={mutation.isPending} />

        {mutation.isPending && (
          <div className="flex flex-col items-center gap-3 py-12 text-muted-foreground">
            <Loader2 className="h-7 w-7 animate-spin" />
            <p className="text-sm">Reading the video and pulling out the ingredients…</p>
          </div>
        )}

        {mutation.isError && !mutation.isPending && (
          <div className="max-w-2xl mx-auto">
            <ErrorBanner error={mutation.error} />
          </div>
        )}

        {data && !mutation.isPending && (
          <div className="space-y-8 animate-fade-in">
            {data.simulated.any && data.notice && <DemoBanner notice={data.notice} />}

            {data.video && (
              <div className="flex flex-col sm:flex-row gap-5 items-start rounded-xl border border-border p-5">
                <Thumbnail src={data.video.thumbnailUrl} />
                <div className="space-y-2">
                  <h2 className="text-xl font-semibold leading-snug">{data.video.title}</h2>
                  {data.video.channelTitle && (
                    <p className="text-muted-foreground">{data.video.channelTitle}</p>
                  )}
                  <div className="flex flex-wrap gap-2 pt-1">
                    <span className="inline-flex items-center gap-1.5 text-xs px-2.5 py-1 rounded-full bg-muted text-muted-foreground">
                      {data.ingredients.length} ingredients
                    </span>
                    <span className="inline-flex items-center gap-1.5 text-xs px-2.5 py-1 rounded-full bg-muted text-muted-foreground">
                      {data.stores.length} stores nearby
                    </span>
                    {data.cached && (
                      <span className="inline-flex items-center gap-1.5 text-xs px-2.5 py-1 rounded-full bg-muted text-muted-foreground">
                        <Zap className="h-3 w-3" />
                        cached
                      </span>
                    )}
                  </div>
                </div>
              </div>
            )}

            <section className="space-y-4">
              <h2 className="text-2xl font-semibold">Shopping list</h2>
              {data.ingredients.length > 0 ? (
                <IngredientsList ingredients={data.ingredients} />
              ) : (
                <p className="text-muted-foreground">
                  No ingredients were listed in this video's description.
                </p>
              )}
            </section>

            <section className="space-y-4">
              <div className="flex items-baseline justify-between gap-4 flex-wrap">
                <h2 className="text-2xl font-semibold">Where to buy</h2>
                <p className="text-sm text-muted-foreground inline-flex items-center gap-1.5">
                  <MapPin className="h-3.5 w-3.5" />
                  near {data.location.label || "you"}
                  {data.location.estimated && " (estimated)"}
                </p>
              </div>
              <StoresList stores={data.stores} />
            </section>
          </div>
        )}
      </div>
    </div>
  );
};

export default Index;
