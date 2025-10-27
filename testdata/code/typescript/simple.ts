import { Logger } from "./logger";
import * as utils from "./utils";

const API_KEY = "test-key-123";
const MAX_RETRIES = 3;

let globalCounter = 0;

type UserId = string;

interface User {
  id: UserId;
  name: string;
  email: string;
}

class UserService {
  private users: User[] = [];

  constructor() {}

  addUser(user: User): void {
    this.users.push(user);
  }

  getUser(id: UserId): User | undefined {
    return this.users.find(u => u.id === id);
  }
}

function validateEmail(email: string): boolean {
  return email.includes("@");
}

export { UserService, User, validateEmail };
