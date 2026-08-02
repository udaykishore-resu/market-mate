import { Card } from "@/components/ui/card";
import { ExternalLink } from "lucide-react";

interface Store {
  name: string;
  address: string;
  distance: string;
  mapUrl: string;
}

interface StoresListProps {
  stores: Store[];
}

export const StoresList = ({ stores }: StoresListProps) => {
  return (
    <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4 animate-fade-in">
      {stores.map((store, index) => (
        <Card key={index} className="p-4 hover:shadow-lg transition-shadow">
          <div className="flex justify-between items-start">
            <div>
              <h3 className="font-semibold text-lg">{store.name}</h3>
              <p className="text-gray-600 text-sm mt-1">{store.address}</p>
              <p className="text-secondary font-medium mt-2">{store.distance}</p>
            </div>
            <a
              href={store.mapUrl}
              target="_blank"
              rel="noopener noreferrer"
              className="text-accent hover:text-accent/80"
            >
              <ExternalLink size={20} />
            </a>
          </div>
        </Card>
      ))}
    </div>
  );
};