import { useState } from "react";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { toast } from "sonner";

interface VideoInputProps {
  onSubmit: (url: string) => void;
}

export const VideoInput = ({ onSubmit }: VideoInputProps) => {
  const [url, setUrl] = useState("");

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!url) {
      toast.error("Please enter a video URL");
      return;
    }
    if (!url.includes("youtube.com") && !url.includes("tiktok.com")) {
      toast.error("Please enter a valid YouTube or TikTok URL");
      return;
    }
    onSubmit(url);
  };

  return (
    <form onSubmit={handleSubmit} className="w-full max-w-2xl mx-auto space-y-4">
      <Input
        type="url"
        placeholder="Paste your recipe video URL here (YouTube or TikTok)"
        value={url}
        onChange={(e) => setUrl(e.target.value)}
        className="w-full text-lg p-6"
      />
      <Button type="submit" className="w-full bg-primary hover:bg-primary/90">
        Find Ingredients
      </Button>
    </form>
  );
};