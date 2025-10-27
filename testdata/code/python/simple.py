"""Simple Python module for testing."""
import os
import sys
from typing import List, Optional

API_KEY = "test-api-key"
MAX_RETRIES = 5
DEBUG_MODE = True

database_url = "postgresql://localhost/testdb"

class User:
    """User model class."""

    def __init__(self, name: str, email: str):
        self.name = name
        self.email = email

    def validate(self) -> bool:
        """Validate user data."""
        return "@" in self.email

    def to_dict(self) -> dict:
        """Convert user to dictionary."""
        return {"name": self.name, "email": self.email}

class UserRepository:
    """Repository for managing users."""

    def __init__(self, db_url: str):
        self.db_url = db_url
        self.users: List[User] = []

    def add(self, user: User) -> None:
        """Add a user to the repository."""
        if user.validate():
            self.users.append(user)

    def find_by_email(self, email: str) -> Optional[User]:
        """Find user by email."""
        for user in self.users:
            if user.email == email:
                return user
        return None

def create_user(name: str, email: str) -> User:
    """Create a new user instance."""
    return User(name, email)
