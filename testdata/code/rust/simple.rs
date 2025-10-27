use std::collections::HashMap;
use std::fmt;

const MAX_USERS: usize = 1000;
const DEFAULT_TIMEOUT: u64 = 30;

static mut GLOBAL_COUNTER: i32 = 0;

#[derive(Debug, Clone)]
pub struct User {
    pub id: String,
    pub name: String,
    pub email: String,
}

impl User {
    pub fn new(id: String, name: String, email: String) -> Self {
        User { id, name, email }
    }

    pub fn validate(&self) -> bool {
        self.email.contains('@')
    }
}

pub trait Repository<T> {
    fn add(&mut self, item: T) -> Result<(), String>;
    fn get(&self, id: &str) -> Option<&T>;
    fn remove(&mut self, id: &str) -> Option<T>;
}

pub struct UserRepository {
    users: HashMap<String, User>,
}

impl UserRepository {
    pub fn new() -> Self {
        UserRepository {
            users: HashMap::new(),
        }
    }

    pub fn size(&self) -> usize {
        self.users.len()
    }
}

impl Repository<User> for UserRepository {
    fn add(&mut self, user: User) -> Result<(), String> {
        if self.users.len() >= MAX_USERS {
            return Err("Repository full".to_string());
        }
        self.users.insert(user.id.clone(), user);
        Ok(())
    }

    fn get(&self, id: &str) -> Option<&User> {
        self.users.get(id)
    }

    fn remove(&mut self, id: &str) -> Option<User> {
        self.users.remove(id)
    }
}

pub fn create_user(id: &str, name: &str, email: &str) -> User {
    User::new(id.to_string(), name.to_string(), email.to_string())
}

pub enum Status {
    Active,
    Inactive,
    Pending,
}
