import { Card } from "@/components/ui/card";

interface Ingredient {
  name: string;
  quantity: string;
}

interface IngredientsListProps {
  ingredients: Ingredient[];
}

export const IngredientsList = ({ ingredients }: IngredientsListProps) => {
  return (
    <div className="grid grid-cols-1 md:grid-cols-2 gap-4 animate-fade-in">
      {ingredients.map((ingredient, index) => (
        <Card key={index} className="p-4 hover:shadow-lg transition-shadow">
          <h3 className="font-semibold text-lg">{ingredient.name}</h3>
          <p className="text-gray-600">{ingredient.quantity}</p>
        </Card>
      ))}
    </div>
  );
};