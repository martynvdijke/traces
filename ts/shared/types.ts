export interface TimelineEvent {
  id: number;
  title: string;
  description: string;
  date: string;
  location: string;
  media_type: string;
  media_url: string;
  thumbnail: string;
  media_caption: string;
  tags: string;
  sort_order: number;
  is_public: boolean;
  is_favorite: boolean;
  created_at: string;
  person_id?: number;
  latitude?: number;
  longitude?: number;
  recurring: string;
  weather_data: string;
  start_time: string;
  end_time: string;
  user_id: number;
  person?: {
    id: number;
    name: string;
    avatar_url: string;
    bio: string;
    birth_date: string;
    color: string;
    created_at: string;
  };
  user?: {
    id: number;
    username: string;
    display_name: string;
    color: string;
    avatar_url: string;
  };
}

export interface CalendarDay {
  date: string;
  events: TimelineEvent[];
  count: number;
}

export interface ContributionMap {
  [date: string]: number;
}

export interface Weather {
  temperature: number;
  condition: string;
  icon: string;
  wind_speed: number;
}
