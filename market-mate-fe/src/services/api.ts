interface RecipeResponse {
    ingredients: {
      name: string;
      quantity: string;
    }[];
    stores: {
      name: string;
      address: string;
      distance: string;
      mapUrl: string;
    }[];
  }
  
  export const processVideoUrl = async (videoUrl: string): Promise<RecipeResponse> => {
    try {
      const response = await fetch('http://localhost:8080/api/process-video', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ url: videoUrl }),
      });
  
      if (!response.ok) {
        throw new Error('Failed to process video');
      }
  
      return await response.json();
    } catch (error) {
      console.error('Error processing video:', error);
      throw error;
    }
  };