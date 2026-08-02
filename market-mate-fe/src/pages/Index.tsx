import { useState } from "react";
import { VideoInput } from "@/components/VideoInput";
import { IngredientsList } from "@/components/IngredientsList";
import { StoresList } from "@/components/StoresList";
import { toast } from "sonner";
import { processVideoUrl } from "@/services/api";
import { useQuery } from "@tanstack/react-query";

const Index = () => {
  const [videoUrl, setVideoUrl] = useState("");
  const [showResults, setShowResults] = useState(false);

  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ['recipe', videoUrl],
    queryFn: () => processVideoUrl(videoUrl),
    enabled: false, // Don't fetch automatically
  });

  const handleVideoSubmit = async (url: string) => {
    setVideoUrl(url);
    setShowResults(true);
    try {
      await refetch();
      toast.success("Video processed successfully!");
    } catch (err) {
      toast.error("Failed to process video. Please try again.");
      setShowResults(false);
    }
  };

  return (
    <div className="min-h-screen bg-background p-6">
      <div className="max-w-7xl mx-auto space-y-12">
        <div className="text-center space-y-4">
          <h1 className="text-4xl font-bold text-primary">Recipe Ingredient Finder</h1>
          <p className="text-xl text-gray-600">
            Paste a recipe video link and we'll help you find the ingredients nearby
          </p>
        </div>

        <VideoInput onSubmit={handleVideoSubmit} />

        {showResults && (
          <div className="space-y-8 animate-fade-in">
            {isLoading && (
              <div className="text-center text-gray-600">
                Processing video...
              </div>
            )}

            {error && (
              <div className="text-center text-red-600">
                Failed to process video. Please try again.
              </div>
            )}

            {data && (
              <>
                {videoUrl && (
                  <div className="aspect-video w-full max-w-2xl mx-auto bg-gray-100 rounded-lg">
                    <div className="h-full flex items-center justify-center text-gray-500">
                      Video Preview
                    </div>
                  </div>
                )}

                <div className="space-y-4">
                  <h2 className="text-2xl font-semibold">Ingredients Needed</h2>
                  <IngredientsList ingredients={data.ingredients} />
                </div>

                <div className="space-y-4">
                  <h2 className="text-2xl font-semibold">Nearby Stores</h2>
                  <StoresList stores={data.stores} />
                </div>
              </>
            )}
          </div>
        )}
      </div>
    </div>
  );
};

export default Index;